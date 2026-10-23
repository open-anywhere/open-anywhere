package greeting

import "testing"

func TestGreet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "空字符串返回 hello world", input: "", expected: "hello world"},
		{name: "普通名字", input: "Alice", expected: "hello, Alice"},
		{name: "包含空格的名字", input: "Bob Smith", expected: "hello, Bob Smith"},
		{name: "中文名字", input: "小明", expected: "hello, 小明"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Greet(tt.input); got != tt.expected {
				t.Errorf("Greet(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
