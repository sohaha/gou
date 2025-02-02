package gou

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/robertkrimen/otto"
	"github.com/robertkrimen/otto/ast"
	"github.com/robertkrimen/otto/parser"
	"github.com/yaoapp/kun/exception"
)

// JavaScriptVM 全局 JavaScript VM
var JavaScriptVM = NewJavaScriptVM()

// NewJavaScriptVM 创建脚本运行环境
func NewJavaScriptVM() ScriptVM {
	return &JavaScript{Otto: otto.New(), Scripts: map[string]*Script{}}
}

// NewScript 创建 Script
func NewScript(file string, source string) (*Script, error) {
	program, err := parser.ParseFile(nil, file, source, 0)
	if err != nil {
		return nil, err
	}
	script := Script{File: file, Source: source, Functions: map[string]Function{}}
	ast.Walk(script, program)
	return &script, nil
}

// Enter 解析脚本文件
func (script Script) Enter(n ast.Node) ast.Visitor {
	if v, ok := n.(*ast.FunctionStatement); ok && v != nil {
		name := v.Function.Name.Name
		idx := int(n.Idx0())
		lines := strings.Split(script.Source[:idx], "\n")
		line := len(lines)
		script.Functions[name] = Function{
			Name:      name,
			Line:      line,
			NumOfArgs: len(v.Function.ParameterList.List),
		}
	}
	return script
}

// Exit 解析脚本文件
func (script Script) Exit(n ast.Node) {}

// MustLoad 加载脚本
func (vm *JavaScript) MustLoad(filename string, name string) ScriptVM {
	err := vm.Load(filename, name)
	if err != nil {
		exception.New("加载脚本 %s 失败 %s", 500, filename, err.Error())
	}
	return vm
}

// MustLoadSource 加载脚本
func (vm *JavaScript) MustLoadSource(filename string, input io.Reader, name string) ScriptVM {
	err := vm.LoadSource(filename, input, name)
	if err != nil {
		exception.New("加载脚本 %s 失败 %s", 500, filename, err.Error())
	}
	return vm
}

// MustGet 读取加载脚本
func (vm *JavaScript) MustGet(name string) *Script {
	script, err := vm.Get(name)
	if err != nil {
		exception.New("%s", 500, name, err.Error())
	}
	return script
}

// Load 加载脚本
func (vm *JavaScript) Load(filename string, name string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	return vm.LoadSource(filename, file, name)
}

// LoadSource 加载内容
func (vm *JavaScript) LoadSource(filename string, input io.Reader, name string) error {

	source, err := ioutil.ReadAll(input)
	if err != nil {
		return err
	}

	script, err := NewScript(filename, string(source))
	if err != nil {
		return err
	}

	err = vm.Compile(script)
	if err != nil {
		return err
	}

	name = strings.ToLower(name)
	vm.Scripts[name] = script
	return nil
}

// Get 读取已载脚本
func (vm *JavaScript) Get(name string) (*Script, error) {
	script, has := vm.Scripts[name]
	if !has {
		return nil, fmt.Errorf("脚本 %s 尚未加载", name)
	}
	return script, nil
}

// Has 检测脚本是否已加载
func (vm *JavaScript) Has(name string) bool {
	_, has := vm.Scripts[name]
	return has
}

// Run 运行指定已加载脚本
func (vm *JavaScript) Run(name string, method string, args ...interface{}) (interface{}, error) {
	script, has := vm.Scripts[name]
	if !has {
		return nil, fmt.Errorf("脚本 %s 尚未加载", name)
	}

	args = append(args, vm.Global) // 添加全局变量
	return vm.RunScript(script, method, args...)
}

// Compile 脚本预编译
func (vm *JavaScript) Compile(script *Script) error {
	// source := ""
	for name, f := range script.Functions {
		numOfArgs := f.NumOfArgs
		argNames := []string{}
		for i := 0; i < numOfArgs; i++ {
			argNames = append(argNames, fmt.Sprintf("arg%d", i))
		}
		call := fmt.Sprintf("%s(%s)", name, strings.Join(argNames, ","))
		compiled, err := vm.Otto.Compile(script.File, fmt.Sprintf("%s\n%s;", script.Source, call))
		if err != nil {
			return err
		}
		f.Compiled = compiled
		script.Functions[name] = f
	}
	return nil
}

// WithSID 设定会话ID
func (vm *JavaScript) WithSID(sid string) ScriptVM {
	vm.Sid = sid
	return vm
}

// WithGlobal 设定全局变量
func (vm *JavaScript) WithGlobal(global map[string]interface{}) ScriptVM {
	vm.Global = global
	return vm
}

// RunScript 运行指定Script
func (vm *JavaScript) RunScript(script *Script, method string, args ...interface{}) (interface{}, error) {

	f, has := script.Functions[method]
	if !has {
		return nil, fmt.Errorf("function %s does not existed! ", method)
	}

	if f.Compiled == nil {
		return nil, fmt.Errorf("function %s does not compiled! ", method)
	}

	newVM := vm.Copy()
	for i, v := range args {
		argName := fmt.Sprintf("arg%d", i)
		newVM.Set(argName, v)
	}

	value, err := newVM.Run(f.Compiled)
	if err != nil {
		if ottoError, ok := err.(*otto.Error); ok {
			return nil, fmt.Errorf("%s", ottoError.String())
		}
		return nil, err
	}

	resp, err := value.Export()
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// WithProcess 支持 Processd 调用
func (vm *JavaScript) WithProcess(allow ...string) ScriptVM {
	vm.Set("Process", func(call otto.FunctionCall) otto.Value {
		name := call.Argument(0).String()
		if name == "" {
			res, _ := vm.ToValue(map[string]interface{}{"code": 400, "message": "缺少处理器名称"})
			return res
		}

		// 更新默认值
		if len(allow) == 0 {
			allow = []string{"*"}
		}

		for i := range allow {
			if allow[i] == "*" {
				break
			}
			if name != allow[i] {
				res, _ := vm.ToValue(map[string]interface{}{"code": 400, "message": fmt.Sprintf("%s 禁止调用", name)})
				return res
			}
		}

		args := []interface{}{}
		for i, in := range call.ArgumentList {
			if i == 0 {
				continue
			}
			arg, _ := in.Export()
			args = append(args, arg)
		}
		// 运行处理器
		p := NewProcess(name, args...).WithGlobal(vm.Global).WithSID(vm.Sid)
		v := p.Run()
		res, _ := vm.ToValue(v)
		return res
	})
	return vm
}
