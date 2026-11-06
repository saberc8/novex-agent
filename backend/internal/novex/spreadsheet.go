package novex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type SheetColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type,omitempty"`
}
type Sheet struct {
	Name        string        `json:"name"`
	HeaderRow   int           `json:"headerRow"`
	RowCount    int           `json:"rowCount"`
	ColumnCount int           `json:"columnCount"`
	Columns     []SheetColumn `json:"columns"`
}
type SheetFilter struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}
type SheetQuery struct {
	Sheet   string        `json:"sheet"`
	Query   string        `json:"query"`
	Filters []SheetFilter `json:"filters"`
	Limit   int           `json:"limit"`
	Offset  int           `json:"offset"`
}

func (s *Server) workbookSheets(ctx context.Context, documentID string) ([]Sheet, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT name,header_row,row_count,columns FROM novex_workbook_sheets WHERE document_id=$1 ORDER BY name`, documentID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Sheet{}
	for rows.Next() {
		var sheet Sheet
		var columns []byte
		if e = rows.Scan(&sheet.Name, &sheet.HeaderRow, &sheet.RowCount, &columns); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(columns, &sheet.Columns); e != nil {
			return nil, e
		}
		sheet.ColumnCount = len(sheet.Columns)
		out = append(out, sheet)
	}
	return out, rows.Err()
}

// Index once on upload; only typed cells and bounded search results reach the agent.
// The original workbook stays private and is never converted to Markdown.
func (s *Server) importSpreadsheet(ctx context.Context, d *Document, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	original, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	d.Kind = "spreadsheet"
	e = tx.QueryRowContext(ctx, `INSERT INTO novex_documents(id,title,file_name,scope,owner_id,tenant_id,department_id,markdown,kind,original) VALUES($1,$2,$3,$4,$5,$6,$7,'','spreadsheet',$8) RETURNING created_at`, d.ID, d.Title, d.FileName, d.Scope, d.OwnerID, d.TenantID, d.DepartmentID, original).Scan(&d.CreatedAt)
	if e != nil {
		return e
	}
	_, source, _, _ := runtime.Caller(0)
	cmd := exec.CommandContext(ctx, env("NOVEX_PYTHON", "python3"), filepath.Join(filepath.Dir(source), "../../scripts/index_spreadsheet.py"), path)
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	decoder := json.NewDecoder(stdout)
	decoder.UseNumber()
	var copyRows *sql.Stmt
	flush := func() error {
		if copyRows == nil {
			return nil
		}
		_, e := copyRows.ExecContext(ctx)
		closeErr := copyRows.Close()
		copyRows = nil
		if e != nil {
			return e
		}
		return closeErr
	}
	defer func() {
		if copyRows != nil {
			_ = copyRows.Close()
		}
	}()
	ended := false
	for {
		var event struct {
			Type      string            `json:"type"`
			Name      string            `json:"name"`
			Sheet     string            `json:"sheet"`
			HeaderRow int               `json:"headerRow"`
			Row       int               `json:"row"`
			RowCount  int               `json:"rowCount"`
			Columns   []SheetColumn     `json:"columns"`
			Cells     map[string]any    `json:"cells"`
			Formulas  map[string]string `json:"formulas"`
			Warnings  []string          `json:"warnings"`
		}
		e = decoder.Decode(&event)
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return e
		}
		switch event.Type {
		case "sheet":
			if e = flush(); e != nil {
				return e
			}
			columns, _ := json.Marshal(event.Columns)
			_, e = tx.ExecContext(ctx, `INSERT INTO novex_workbook_sheets(document_id,name,header_row,columns) VALUES($1,$2,$3,$4)`, d.ID, event.Name, event.HeaderRow, string(columns))
			if e != nil {
				return e
			}
			d.SheetCount++
			copyRows, e = tx.PrepareContext(ctx, pq.CopyIn("novex_workbook_rows", "document_id", "sheet_name", "row_number", "cells", "formulas", "search_text"))
			if e != nil {
				return e
			}
		case "row":
			if copyRows == nil {
				return errors.New("missing sheet")
			}
			cells, _ := json.Marshal(event.Cells)
			formulas, _ := json.Marshal(event.Formulas)
			var searchable strings.Builder
			for _, v := range event.Cells {
				searchable.WriteString(fmt.Sprint(v))
				searchable.WriteByte('\n')
			}
			_, e = copyRows.ExecContext(ctx, d.ID, event.Sheet, event.Row, string(cells), string(formulas), searchable.String())
			if e != nil {
				return e
			}
		case "endSheet":
			if e = flush(); e != nil {
				return e
			}
			columns, _ := json.Marshal(event.Columns)
			_, e = tx.ExecContext(ctx, `UPDATE novex_workbook_sheets SET row_count=$3,columns=$4 WHERE document_id=$1 AND name=$2`, d.ID, event.Name, event.RowCount, string(columns))
			if e != nil {
				return e
			}
		case "end":
			d.RowCount = event.RowCount
			d.Warnings = event.Warnings
			ended = true
		default:
			return errors.New("invalid index stream")
		}
	}
	e = cmd.Wait()
	finished = true
	if e != nil || !ended {
		return errors.New("spreadsheet indexing failed")
	}
	if e = flush(); e != nil {
		return e
	}
	warnings, _ := json.Marshal(d.Warnings)
	if string(warnings) == "null" {
		warnings = []byte("[]")
	}
	_, e = tx.ExecContext(ctx, `UPDATE novex_documents SET sheet_count=$2,row_count=$3,warnings=$4 WHERE id=$1`, d.ID, d.SheetCount, d.RowCount, string(warnings))
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Server) querySpreadsheet(ctx context.Context, u *User, documentID string, q SheetQuery) (map[string]any, error) {
	d, e := s.document(ctx, u, documentID)
	if e != nil || d.Kind != "spreadsheet" {
		return nil, ErrDenied
	}
	if len(q.Query) > 200 || len(q.Filters) > 8 || q.Offset < 0 || q.Offset > 200000 {
		return nil, fmt.Errorf("查询过长或超出分页范围")
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 20
	}
	var selected *Sheet
	for i := range d.Sheets {
		if d.Sheets[i].Name == q.Sheet {
			selected = &d.Sheets[i]
		}
	}
	if q.Sheet != "" && selected == nil {
		return nil, fmt.Errorf("工作表不存在")
	}
	if len(q.Filters) > 0 && selected == nil {
		return nil, fmt.Errorf("按列筛选必须指定工作表")
	}
	args := []any{documentID}
	where := []string{"document_id=$1"}
	param := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if q.Sheet != "" {
		where = append(where, "sheet_name="+param(q.Sheet))
	}
	if q.Query != "" {
		where = append(where, "search_text ILIKE "+param("%"+escapeLike(q.Query)+"%")+` ESCAPE '\'`)
	}
	for _, filter := range q.Filters {
		switch value := filter.Value.(type) {
		case string:
			if len(value) > 200 {
				return nil, fmt.Errorf("筛选值最多200字节")
			}
		case nil, bool, json.Number:
		case float64:
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("筛选值必须为有限数值")
			}
		default:
			return nil, fmt.Errorf("筛选值必须为文本、数字或布尔值")
		}
		var column *SheetColumn
		for i := range selected.Columns {
			if selected.Columns[i].Key == filter.Column {
				column = &selected.Columns[i]
			}
		}
		if column == nil {
			return nil, fmt.Errorf("列不存在")
		}
		if filter.Op == "eq" {
			value := filter.Value
			if column.Type == "number" {
				number, e := strconv.ParseFloat(fmt.Sprint(value), 64)
				if e != nil || math.IsNaN(number) || math.IsInf(number, 0) {
					return nil, fmt.Errorf("数值筛选值不合法")
				}
				value = number
			}
			if column.Type == "boolean" {
				boolean, e := strconv.ParseBool(fmt.Sprint(value))
				if e != nil {
					return nil, fmt.Errorf("布尔筛选值不合法")
				}
				value = boolean
			}
			encoded, e := json.Marshal(map[string]any{column.Key: value})
			if e != nil {
				return nil, ErrDenied
			}
			where = append(where, "cells @> "+param(string(encoded))+"::jsonb")
		} else if filter.Op == "contains" {
			where = append(where, "(cells ->> "+param(column.Key)+") ILIKE "+param("%"+escapeLike(fmt.Sprint(filter.Value))+"%")+` ESCAPE '\'`)
		} else if filter.Op == "gte" || filter.Op == "lte" {
			if column.Type != "number" {
				return nil, fmt.Errorf("大小比较仅支持数值列")
			}
			number, e := strconv.ParseFloat(fmt.Sprint(filter.Value), 64)
			if e != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, fmt.Errorf("数值筛选值不合法")
			}
			key := param(column.Key)
			operator := " >= "
			if filter.Op == "lte" {
				operator = " <= "
			}
			where = append(where, "(CASE WHEN jsonb_typeof(cells -> "+key+")='number' THEN (cells ->> "+key+")::numeric END)"+operator+param(number))
		} else {
			return nil, fmt.Errorf("不支持的筛选条件")
		}
	}
	limit := param(q.Limit + 1)
	offset := param(q.Offset)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, e := s.DB.QueryContext(ctx, `SELECT sheet_name,row_number,cells,formulas FROM novex_workbook_rows WHERE `+strings.Join(where, " AND ")+` ORDER BY sheet_name,row_number LIMIT `+limit+` OFFSET `+offset, args...)
	if e != nil {
		return nil, fmt.Errorf("表格查询失败或超时")
	}
	defer rows.Close()
	out := []map[string]any{}
	responseBytes := 0
	for rows.Next() {
		var name string
		var number int
		var raw, formula []byte
		if e = rows.Scan(&name, &number, &raw, &formula); e != nil {
			return nil, e
		}
		responseBytes += len(raw) + len(formula)
		if responseBytes > 512<<10 {
			return nil, fmt.Errorf("结果超过512KB，请减少返回行数或使用更精确的筛选条件")
		}
		var cells map[string]any
		var formulas map[string]string
		_ = json.Unmarshal(raw, &cells)
		_ = json.Unmarshal(formula, &formulas)
		last := "A"
		for _, sheet := range d.Sheets {
			if sheet.Name == name && len(sheet.Columns) > 0 {
				last = sheet.Columns[len(sheet.Columns)-1].Key
			}
		}
		out = append(out, map[string]any{"sheet": name, "row": number, "cells": cells, "formulas": formulas, "citation": fmt.Sprintf("[document:%s] %s!A%d:%s%d", documentID, name, number, last, number)})
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	hasMore := len(out) > q.Limit
	if hasMore {
		out = out[:q.Limit]
	}
	columns := []SheetColumn{}
	if selected != nil {
		columns = selected.Columns
	}
	s.audit(ctx, u.ID, "spreadsheet.search", documentID, true, fmt.Sprintf("%d authorized rows", len(out)))
	return map[string]any{"documentId": documentID, "sheet": q.Sheet, "columns": columns, "rows": out, "hasMore": hasMore, "warnings": d.Warnings}, nil
}
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
func (s *Server) searchSpreadsheetHTTP(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var q SheetQuery
	if !bind(c, &q) {
		return
	}
	out, e := s.querySpreadsheet(c, actor(c), c.Param("id"), q)
	if e != nil {
		if errors.Is(e, ErrDenied) {
			fail(c, 404, "表格不可访问")
		} else {
			fail(c, 400, e.Error())
		}
		return
	}
	ok(c, out)
}
func (s *Server) downloadSpreadsheet(c *gin.Context) {
	d, e := s.document(c, actor(c), c.Param("id"))
	if e != nil || d.Kind != "spreadsheet" {
		fail(c, 404, "文件不可访问")
		return
	}
	var original []byte
	if e = s.DB.QueryRowContext(c, `SELECT original FROM novex_documents WHERE id=$1`, d.ID).Scan(&original); e != nil {
		fail(c, 404, "文件不可访问")
		return
	}
	contentType := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	if strings.EqualFold(filepath.Ext(d.FileName), ".csv") {
		contentType = "text/csv; charset=utf-8"
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.FileName}))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, contentType, original)
}
