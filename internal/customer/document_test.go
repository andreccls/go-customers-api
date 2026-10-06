package customer

import "testing"

func TestNormalizeDocument(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"valid CPF digits", "52998224725", "52998224725", true},
		{"valid CPF formatted", "529.982.247-25", "52998224725", true},
		{"CPF wrong check digit", "52998224726", "52998224726", false},
		{"CPF second digit wrong", "52998224735", "52998224735", false},
		{"CPF all same digits", "11111111111", "11111111111", false},
		{"valid CNPJ formatted", "11.222.333/0001-81", "11222333000181", true},
		{"valid CNPJ digits", "11222333000181", "11222333000181", true},
		{"CNPJ wrong check digit", "11222333000182", "11222333000182", false},
		{"CNPJ second digit wrong", "11222333000191", "11222333000191", false},
		{"CNPJ all same digits", "00000000000000", "00000000000000", false},
		{"wrong length", "1234567890", "1234567890", false},
		{"letters rejected", "5299822472a", "5299822472", false},
		{"empty", "", "", false},
		{"CPF whose check digits are 0 (remainder < 2)", "00000003700", "00000003700", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeDocument(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Errorf("NormalizeDocument(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
