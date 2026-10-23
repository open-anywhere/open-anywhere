// Package greeting 提供最基础的问候语生成逻辑。
package greeting

import "fmt"

// Greet 返回对 name 的问候语；name 为空时返回通用的 "hello world"。
func Greet(name string) string {
	if name == "" {
		return "hello world"
	}
	return fmt.Sprintf("hello, %s", name)
}
