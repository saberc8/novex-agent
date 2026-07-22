package http

import (
	"strings"
	"testing"
)

func TestValidatePasswordHonorsConfiguredPolicy(t *testing.T) {
	policy := passwordPolicy{
		MinLength:      10,
		AllowUsername:  false,
		RequireSymbols: true,
	}
	tests := []struct {
		name     string
		password string
		username string
		wantErr  bool
	}{
		{name: "valid", password: "Strong-123!", username: "operator", wantErr: false},
		{name: "too short", password: "Aa1!", username: "operator", wantErr: true},
		{name: "missing digit", password: "Strong-Pass!", username: "operator", wantErr: true},
		{name: "missing symbol", password: "StrongPass123", username: "operator", wantErr: true},
		{name: "contains username", password: "Operator-123!", username: "operator", wantErr: true},
		{name: "contains reversed username", password: "Rotarepo-123!", username: "operator", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePassword(test.password, test.username, policy)
			if (err != nil) != test.wantErr {
				t.Fatalf("validatePassword() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestParseUserImportCSV(t *testing.T) {
	rows, err := parseUserImportCSV(strings.NewReader("\ufeffusername,nickname,password,gender,email,phone\noperator,操作员,Strong-123!,1,operator@example.com,13800000000\n"))
	if err != nil {
		t.Fatalf("parseUserImportCSV() error = %v", err)
	}
	if len(rows) != 1 || rows[0].Username != "operator" || rows[0].Gender != 1 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestParseUserImportCSVRejectsUnexpectedHeader(t *testing.T) {
	_, err := parseUserImportCSV(strings.NewReader("username,nickname\noperator,操作员\n"))
	if err == nil {
		t.Fatal("expected invalid header error")
	}
}
