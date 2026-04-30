package main

import (
	"flag"
	"fmt"
	"runtime"
)

// runVersion 执行 version 命令，打印版本信息。
//
// 用法：
//
//	scrapy-go version [-v]
//
// 选项：
//
//	-v  显示详细版本信息（包括 Go 版本和平台信息）
func runVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.Usage = printVersionUsage

	verbose := fs.Bool("v", false, "显示详细信息（Go 版本、操作系统、架构）")

	if err := fs.Parse(args); err != nil {
		// -h/--help 由 flag 自动处理并输出 Usage
		return
	}

	if *verbose {
		fmt.Printf("scrapy-go : %s\n", Version)
		fmt.Printf("Go        : %s\n", runtime.Version())
		fmt.Printf("OS/Arch   : %s/%s\n", runtime.GOOS, runtime.GOARCH)
	} else {
		fmt.Printf("scrapy-go %s\n", Version)
	}
}

// printVersionUsage 打印 version 命令的帮助信息。
func printVersionUsage() {
	fmt.Println(`用法: scrapy-go version [-v]

打印 scrapy-go 版本信息。

选项:
  -v    显示详细信息（Go 版本、操作系统、架构）`)
}
