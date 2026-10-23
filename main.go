package main

import (
	"fmt"
	"io"
	"os"

	"github.com/open-anywhere/open-anywhere/internal/greeting"
)

// run 将问候语写入 out，便于测试。
func run(out io.Writer) {
	fmt.Fprintln(out, greeting.Greet(""))
}

func main() {
	run(os.Stdout)
}
