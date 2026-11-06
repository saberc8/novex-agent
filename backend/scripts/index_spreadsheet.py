#!/usr/bin/env python3
"""Stream bounded XLSX/CSV records without converting the workbook to Markdown.

The first non-empty row is the header; column letters remain stable even with
duplicate/blank labels. Formulas are never executed. Cached Excel values are
read and missing caches are reported explicitly. Output is private NDJSON.
"""
import csv
import datetime
import json
import math
import pathlib
import sys
import zipfile

MAX_ROWS = 200_000
MAX_CELLS = 2_000_000
MAX_COLUMNS = 128
MAX_SHEETS = 32
row_count = 0
cell_count = 0
warnings = []


def emit(value):
    print(json.dumps(value, ensure_ascii=False, allow_nan=False), flush=False)


def key(index):
    value = ''
    while index:
        index, remainder = divmod(index - 1, 26)
        value = chr(65 + remainder) + value
    return value


def normalized(value):
    if isinstance(value, (datetime.datetime, datetime.date, datetime.time)):
        return value.isoformat()
    if isinstance(value, float) and not math.isfinite(value):
        return None
    if isinstance(value, str):
        value = value.replace('\x00', '')
        if len(value) > 16_384:
            raise ValueError('A cell exceeds 16384 characters')
    return value


def sheet(name, rows):
    global row_count, cell_count
    columns = None
    sheet_rows = 0
    types = {}
    for number, values, formulas in rows:
        values = list(values)
        if all(value is None or (isinstance(value, str) and not value.strip()) for value in values) and not formulas:
            continue
        while values and values[-1] is None and not formulas.get(key(len(values))):
            values.pop()
        if not values and not formulas:
            continue
        if len(values) > MAX_COLUMNS:
            raise ValueError('A sheet exceeds 128 columns')
        if columns is None:
            columns = [{'key': key(i + 1), 'label': str(normalized(value) or key(i + 1))} for i, value in enumerate(values)]
            emit({'type': 'sheet', 'name': name, 'headerRow': number, 'columns': columns})
            continue
        if len(values) > len(columns):
            raise ValueError('Data extends beyond the header; include a header for every column')
        cells = {key(i + 1): normalized(value) for i, value in enumerate(values) if value is not None}
        for i, value in enumerate(values):
            if value is None:
                continue
            kind = 'boolean' if isinstance(value, bool) else 'number' if isinstance(value, (int, float)) else 'date' if isinstance(value, (datetime.datetime, datetime.date, datetime.time)) else 'string'
            types.setdefault(key(i + 1), set()).add(kind)
        if not cells and not formulas:
            continue
        row_count += 1
        sheet_rows += 1
        cell_count += len(cells) + len(formulas)
        if row_count > MAX_ROWS or cell_count > MAX_CELLS:
            raise ValueError('Workbook exceeds 200000 data rows or 2000000 cells')
        for column, formula in formulas.items():
            if column not in cells and len(warnings) < 20:
                warnings.append(f'{name}!{column}{number}: 公式没有保存的计算结果；请在 Excel 中重新计算并保存后再上传。')
        emit({'type': 'row', 'sheet': name, 'row': number, 'cells': cells, 'formulas': formulas})
    if columns is None:
        emit({'type': 'sheet', 'name': name, 'headerRow': 0, 'columns': []})
    for column in columns or []:
        found = types.get(column['key'], set())
        column['type'] = next(iter(found)) if len(found) == 1 else 'mixed' if found else 'string'
    emit({'type': 'endSheet', 'name': name, 'rowCount': sheet_rows, 'columns': columns or []})


def main():
    path = pathlib.Path(sys.argv[1])
    if path.stat().st_size > 32 * 1024 * 1024:
        raise ValueError('File exceeds 32 MiB')
    if path.suffix.lower() == '.csv':
        csv.field_size_limit(65536)
        with path.open(encoding='utf-8-sig', newline='') as stream:
            sample = stream.read(8192)
            stream.seek(0)
            try:
                dialect = csv.Sniffer().sniff(sample, delimiters=',;\t')
            except csv.Error:
                dialect = csv.excel
            sheet('Sheet1', ((i, values, {}) for i, values in enumerate(csv.reader(stream, dialect), 1)))
    elif path.suffix.lower() == '.xlsx':
        with zipfile.ZipFile(path) as archive:
            members = archive.infolist()
            if len(members) > 2048 or sum(m.file_size for m in members) > 256 * 1024 * 1024 or any(m.file_size > 128 * 1024 * 1024 for m in members):
                raise ValueError('Expanded workbook is too large')
            if any('vbaProject' in m.filename for m in members):
                raise ValueError('Macro-enabled workbooks are not supported')
        from openpyxl import load_workbook
        values_book = load_workbook(path, read_only=True, data_only=True, keep_links=False)
        formula_book = load_workbook(path, read_only=True, data_only=False, keep_links=False)
        try:
            if len(values_book.worksheets) > MAX_SHEETS:
                raise ValueError('Workbook exceeds 32 sheets')
            for worksheet, formulas in zip(values_book.worksheets, formula_book.worksheets):
                # Some exporters save a stale A1 dimension despite real rows.
                # Read the actual XML records and enforce our own row/cell bounds.
                worksheet.reset_dimensions()
                formulas.reset_dimensions()
                def records():
                    for number, (values, raw) in enumerate(zip(worksheet.iter_rows(values_only=True), formulas.iter_rows()), 1):
                        if number > MAX_ROWS + 1000:
                            raise ValueError('Sheet row span is too large')
                        formula_values = {key(i + 1): c.value for i, c in enumerate(raw) if c.data_type == 'f'}
                        yield number, values, formula_values
                sheet(worksheet.title, records())
        finally:
            values_book.close()
            formula_book.close()
    else:
        raise ValueError('Only XLSX and UTF-8 CSV are supported')
    emit({'type': 'end', 'rowCount': row_count, 'warnings': warnings})


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Never print cell content or file paths on a conversion error.
        print(f'Spreadsheet indexing failed: {type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
