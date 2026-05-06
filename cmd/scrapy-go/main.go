// Package main 实现 scrapy-go 命令行脚手架工具。
//
// 提供项目创建、爬虫生成和版本查看等功能。
// 使用标准库 flag 包实现子命令参数解析。
//
// 用法：
//
//	scrapy-go <command> [arguments]
//
// 可用命令：
//
//	startproject  创建新的 scrapy-go 项目
//	genspider     使用模板生成新的爬虫文件
//	version       打印版本信息
package main

import (
	"fmt"
	"os"
)

// Version 是当前 scrapy-go 框架版本号。
const Version = "0.5.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "startproject":
		if err := runStartProject(args); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "genspider":
		if err := runGenSpider(args); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "generate-adapter":
		if err := runGenerateAdapter(args); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
	case "version":
		runVersion(args)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

// printUsage 打印工具的使用帮助信息。
func printUsage() {
	fmt.Println(`scrapy-go - Go 语言 Web 爬虫框架脚手架工具

用法:
  scrapy-go <command> [arguments]

可用命令:
  startproject <name> [dir]    创建新的 scrapy-go 项目
  genspider <name> <domain>    使用模板生成新的爬虫文件
  generate-adapter -type=Name  从 struct 生成 ItemAdapter 实现
  version [-v]                 打印版本信息

使用 "scrapy-go <command> -h" 获取命令的详细帮助。`)
}
