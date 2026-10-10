package model

import "testing"

func TestParseMoney(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Money
		wantErr bool
	}{
		{name: "zero", input: "0", want: 0},
		{name: "whole", input: "10", want: 1000},
		{name: "one decimal", input: "10.5", want: 1050},
		{name: "two decimals", input: "10.25", want: 1025},
		{name: "one hundredth", input: "0.01", want: 1},
		{name: "trailing zeros", input: "10.2500", want: 1025},
		{name: "negative", input: "-10.25", want: -1025},
		{name: "maximum int64", input: "92233720368547758.07", want: Money(9223372036854775807)},
		{name: "minimum int64", input: "-92233720368547758.08", want: Money(-9223372036854775808)},
		{name: "empty", input: "", wantErr: true},
		{name: "letters", input: "abc", wantErr: true},
		{name: "plus sign", input: "+10", wantErr: true},
		{name: "double minus", input: "--10", wantErr: true},
		{name: "comma", input: "10,25", wantErr: true},
		{name: "missing whole", input: ".25", wantErr: true},
		{name: "missing fraction", input: "10.", wantErr: true},
		{name: "too precise", input: "0.001", wantErr: true},
		{name: "multiple dots", input: "1.2.3", wantErr: true},
		{name: "whitespace", input: " 10", wantErr: true},
		{name: "overflow", input: "92233720368547758.08", wantErr: true},
		{name: "negative overflow", input: "-92233720368547758.09", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMoney(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMoney(%q): ожидалась ошибка", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMoney(%q): неожиданная ошибка: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseMoney(%q) = %d сотых, ожидалось %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestMoneyString(t *testing.T) {
	tests := []struct {
		name  string
		input Money
		want  string
	}{
		{name: "zero", input: 0, want: "0.00"},
		{name: "one hundredth", input: 1, want: "0.01"},
		{name: "less than one", input: 50, want: "0.50"},
		{name: "whole", input: 1000, want: "10.00"},
		{name: "fraction", input: 1025, want: "10.25"},
		{name: "negative", input: -1025, want: "-10.25"},
		{name: "maximum int64", input: Money(9223372036854775807), want: "92233720368547758.07"},
		{name: "minimum int64", input: Money(-9223372036854775808), want: "-92233720368547758.08"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.input.String()
			if got != tt.want {
				t.Errorf("Money(%d).String() = %q, ожидалось %q", tt.input, got, tt.want)
			}
		})
	}
}
