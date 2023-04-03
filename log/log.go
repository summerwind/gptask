package log

import (
	"fmt"
	"log"

	"github.com/logrusorgru/aurora/v4"
)

var EnableDebug bool

func Comment(comment string) {
	fmt.Println(aurora.Gray(12, fmt.Sprintf("# %s", comment)))
}

func Command(cmd string) {
	fmt.Printf("$ %s\n", cmd)
}

func CodeBlock(code string) {
	fmt.Printf("```\n%s\n```\n", code)
}

func Stdout(line string) {
	fmt.Println(line)
}

func Stderr(line string) {
	fmt.Println(line)
}

func Debug(label, data string) {
	if !EnableDebug {
		return
	}

	log.Printf("%s: %s\n", label, data)
}
