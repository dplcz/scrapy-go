// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/dplcz/scrapy-go/pkg/selector"
)

// ============================================================================
// 表单定位选项（P3-012b）
// ============================================================================

// FormLocator 定义表单定位选项，用于在 HTML 页面中定位目标 <form> 元素。
//
// 定位优先级（与 Scrapy 一致）：
//  1. formname — 按 name 属性匹配
//  2. formid — 按 id 属性匹配
//  3. formxpath / formcss — 按 XPath 或 CSS 选择器匹配
//  4. formnumber — 按出现顺序（从 0 开始）
type FormLocator struct {
	// FormName 按 <form name="..."> 属性定位。
	FormName string

	// FormID 按 <form id="..."> 属性定位。
	FormID string

	// FormNumber 按表单在页面中的出现顺序定位（从 0 开始，默认 0）。
	FormNumber int

	// FormXPath 按 XPath 表达式定位表单或表单内元素。
	// 如果匹配的元素不是 <form>，会向上查找最近的 <form> 祖先。
	FormXPath string

	// FormCSS 按 CSS 选择器定位表单或表单内元素。
	// 内部转换为 XPath 后执行查找。
	FormCSS string
}

// FormOption 是 FormRequestFromResponse 的配置选项。
type FormOption func(*formConfig)

// formConfig 存储 FormRequestFromResponse 的所有配置。
type formConfig struct {
	locator   FormLocator
	formdata  map[string][]string
	dontClick bool
	clickdata map[string]string
	reqOpts   []RequestOption
}

// WithFormName 按 name 属性定位表单。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormName("login"),
//	)
func WithFormName(name string) FormOption {
	return func(c *formConfig) {
		c.locator.FormName = name
	}
}

// WithFormID 按 id 属性定位表单。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormID("login-form"),
//	)
func WithFormID(id string) FormOption {
	return func(c *formConfig) {
		c.locator.FormID = id
	}
}

// WithFormNumber 按表单在页面中的出现顺序定位（从 0 开始）。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormNumber(1), // 第二个表单
//	)
func WithFormNumber(n int) FormOption {
	return func(c *formConfig) {
		c.locator.FormNumber = n
	}
}

// WithFormXPath 按 XPath 表达式定位表单。
// 如果匹配的元素不是 <form>，会向上查找最近的 <form> 祖先。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormXPath("//form[@class='main']"),
//	)
func WithFormXPath(xpath string) FormOption {
	return func(c *formConfig) {
		c.locator.FormXPath = xpath
	}
}

// WithFormCSS 按 CSS 选择器定位表单。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormCSS("form.login-form"),
//	)
func WithFormCSS(css string) FormOption {
	return func(c *formConfig) {
		c.locator.FormCSS = css
	}
}

// WithFormData 设置要提交的表单数据。
// 这些值会覆盖从 HTML 表单中提取的同名字段。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormData(map[string][]string{
//	        "username": {"admin"},
//	        "password": {"secret"},
//	    }),
//	)
func WithFormResponseData(formdata map[string][]string) FormOption {
	return func(c *formConfig) {
		c.formdata = formdata
	}
}

// WithDontClick 禁止自动点击提交按钮。
// 默认情况下，FormRequestFromResponse 会自动包含第一个提交按钮的 name/value。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithDontClick(),
//	)
func WithDontClick() FormOption {
	return func(c *formConfig) {
		c.dontClick = true
	}
}

// WithClickButton 指定要点击的提交按钮。
// 通过按钮的属性（如 name、value、type）来定位。
// 舍弃 Scrapy 的坐标点击（nr 参数），仅支持属性匹配。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithClickButton(map[string]string{"name": "submit", "value": "login"}),
//	)
func WithClickButton(clickdata map[string]string) FormOption {
	return func(c *formConfig) {
		c.clickdata = clickdata
	}
}

// WithRequestOptions 设置传递给底层 NewFormRequest 的请求选项。
//
// 用法：
//
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithRequestOptions(
//	        http.WithCallback(myCallback),
//	        http.WithMeta(map[string]any{"key": "value"}),
//	    ),
//	)
func WithRequestOptions(opts ...RequestOption) FormOption {
	return func(c *formConfig) {
		c.reqOpts = append(c.reqOpts, opts...)
	}
}

// ============================================================================
// FormRequestFromResponse（P3-012a）
// ============================================================================

// FormRequestFromResponse 从 HTTP 响应中自动提取 HTML <form> 信息并创建表单请求。
//
// 对齐 Scrapy 的 FormRequest.from_response()，基于 pkg/selector 解析 HTML。
//
// 功能：
//   - 自动提取 <form> 的 action（URL）、method（HTTP 方法）
//   - 自动收集表单内所有 <input>、<select>、<textarea> 的 name/value
//   - 支持通过 FormOption 定位特定表单和覆盖字段值
//   - 支持自动点击提交按钮（默认包含第一个 submit 按钮的 name/value）
//
// 表单定位优先级：
//  1. WithFormName — 按 name 属性
//  2. WithFormID — 按 id 属性
//  3. WithFormXPath / WithFormCSS — 按选择器
//  4. WithFormNumber — 按出现顺序（默认第 0 个）
//
// 用法：
//
//	// 自动提取第一个表单
//	req, err := http.FormRequestFromResponse(resp)
//
//	// 指定表单并填写数据
//	req, err := http.FormRequestFromResponse(resp,
//	    http.WithFormID("login-form"),
//	    http.WithFormResponseData(map[string][]string{
//	        "username": {"admin"},
//	        "password": {"secret"},
//	    }),
//	    http.WithRequestOptions(
//	        http.WithCallback(myCallback),
//	    ),
//	)
func FormRequestFromResponse(resp *Response, opts ...FormOption) (*Request, error) {
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}

	// 解析配置
	cfg := &formConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 创建 Selector
	sel := resp.Selector()

	// 定位表单
	formSel, err := locateForm(sel, cfg.locator)
	if err != nil {
		return nil, fmt.Errorf("form not found in %s: %w", resp.URL.String(), err)
	}

	// 提取表单 action URL
	formURL, err := extractFormURL(resp, formSel)
	if err != nil {
		return nil, fmt.Errorf("failed to extract form URL: %w", err)
	}

	// 提取表单 method
	method := extractFormMethod(formSel)

	// 提取表单字段
	formdata := extractFormInputs(formSel, cfg.formdata, cfg.dontClick, cfg.clickdata)

	// 构建请求选项
	reqOpts := []RequestOption{WithMethod(method)}
	reqOpts = append(reqOpts, cfg.reqOpts...)

	return NewFormRequest(formURL, formdata, reqOpts...)
}

// ============================================================================
// 内部实现
// ============================================================================

// locateForm 在 HTML 中定位目标 <form> 元素。
func locateForm(sel *selector.Selector, loc FormLocator) (*selector.Selector, error) {
	// 获取所有表单
	forms := sel.CSS("form")
	if forms.Len() == 0 {
		return nil, fmt.Errorf("no <form> element found")
	}

	// 1. 按 name 属性定位
	if loc.FormName != "" {
		found := sel.CSS(fmt.Sprintf("form[name=%q]", loc.FormName))
		if found.Len() > 0 {
			return found.First(), nil
		}
		return nil, fmt.Errorf("no <form> with name=%q found", loc.FormName)
	}

	// 2. 按 id 属性定位
	if loc.FormID != "" {
		found := sel.CSS(fmt.Sprintf("form#%s", loc.FormID))
		if found.Len() > 0 {
			return found.First(), nil
		}
		return nil, fmt.Errorf("no <form> with id=%q found", loc.FormID)
	}

	// 3. 按 CSS 选择器定位（转换为直接 CSS 查询）
	if loc.FormCSS != "" {
		found := sel.CSS(loc.FormCSS)
		if found.Len() > 0 {
			first := found.First()
			// 检查是否是 <form> 元素，如果不是则查找最近的 <form> 祖先
			html := first.GetHTML()
			if strings.HasPrefix(strings.TrimSpace(strings.ToLower(html)), "<form") {
				return first, nil
			}
			// 不是 <form>，尝试在整个文档中查找包含该元素的表单
			// 由于 goquery 不支持向上遍历，使用 XPath 替代
			return locateFormByXPathAncestor(sel, loc.FormCSS)
		}
		return nil, fmt.Errorf("no element matching CSS %q found", loc.FormCSS)
	}

	// 4. 按 XPath 表达式定位
	if loc.FormXPath != "" {
		found := sel.XPath(loc.FormXPath)
		if found.Len() > 0 {
			first := found.First()
			html := first.GetHTML()
			if strings.HasPrefix(strings.TrimSpace(strings.ToLower(html)), "<form") {
				return first, nil
			}
			// 不是 <form>，向上查找 <form> 祖先
			ancestorXPath := loc.FormXPath + "/ancestor::form[1]"
			ancestorForms := sel.XPath(ancestorXPath)
			if ancestorForms.Len() > 0 {
				return ancestorForms.First(), nil
			}
			return nil, fmt.Errorf("no <form> ancestor found for XPath %q", loc.FormXPath)
		}
		return nil, fmt.Errorf("no element matching XPath %q found", loc.FormXPath)
	}

	// 5. 按出现顺序定位（默认第 0 个）
	if loc.FormNumber >= forms.Len() {
		return nil, fmt.Errorf("form number %d not found (total: %d)", loc.FormNumber, forms.Len())
	}
	return forms[loc.FormNumber], nil
}

// locateFormByXPathAncestor 通过 XPath ancestor 轴查找包含 CSS 匹配元素的表单。
func locateFormByXPathAncestor(sel *selector.Selector, css string) (*selector.Selector, error) {
	// 回退策略：遍历所有表单，检查哪个包含匹配元素
	forms := sel.CSS("form")
	for i := 0; i < forms.Len(); i++ {
		form := forms[i]
		if form.CSS(css).Len() > 0 {
			return form, nil
		}
	}
	return nil, fmt.Errorf("no <form> containing element matching CSS %q found", css)
}

// extractFormURL 从 <form> 元素中提取 action URL。
func extractFormURL(resp *Response, formSel *selector.Selector) (string, error) {
	action, hasAction := formSel.Attr("action")

	if !hasAction || strings.TrimSpace(action) == "" {
		// 没有 action 属性，使用当前页面 URL
		return resp.URL.String(), nil
	}

	// 去除 HTML5 空白字符
	action = strings.TrimSpace(action)

	// 解析为绝对 URL
	actionURL, err := url.Parse(action)
	if err != nil {
		return "", fmt.Errorf("invalid action URL %q: %w", action, err)
	}

	return resp.URL.ResolveReference(actionURL).String(), nil
}

// extractFormMethod 从 <form> 元素中提取 HTTP 方法。
func extractFormMethod(formSel *selector.Selector) string {
	method, hasMethod := formSel.Attr("method")
	if !hasMethod || strings.TrimSpace(method) == "" {
		return "GET" // HTML 默认 GET
	}

	method = strings.ToUpper(strings.TrimSpace(method))

	// 只允许 GET 和 POST（与 Scrapy 一致）
	switch method {
	case "GET", "POST":
		return method
	default:
		return "GET"
	}
}

// extractFormInputs 从 <form> 元素中提取所有表单字段的 name/value。
func extractFormInputs(formSel *selector.Selector, userFormdata map[string][]string, dontClick bool, clickdata map[string]string) map[string][]string {
	// 用户提供的字段名集合（这些字段不从 HTML 中提取）
	userKeys := make(map[string]bool)
	for k := range userFormdata {
		userKeys[k] = true
	}

	result := make(map[string][]string)

	// 提取 <input> 字段
	extractInputFields(formSel, result, userKeys)

	// 提取 <select> 字段
	extractSelectFields(formSel, result, userKeys)

	// 提取 <textarea> 字段
	extractTextareaFields(formSel, result, userKeys)

	// 处理提交按钮
	if !dontClick {
		addClickable(formSel, result, userKeys, clickdata)
	}

	// 合并用户提供的数据（覆盖 HTML 中提取的值）
	for k, v := range userFormdata {
		result[k] = v
	}

	return result
}

// extractInputFields 提取 <input> 元素的 name/value。
func extractInputFields(formSel *selector.Selector, result map[string][]string, userKeys map[string]bool) {
	inputs := formSel.CSS("input")
	for _, input := range inputs {
		name, hasName := input.Attr("name")
		if !hasName || name == "" || userKeys[name] {
			continue
		}

		inputType, _ := input.Attr("type")
		inputType = strings.ToLower(strings.TrimSpace(inputType))

		// 跳过提交/图片/重置按钮
		switch inputType {
		case "submit", "image", "reset":
			continue
		case "checkbox", "radio":
			// 只包含被选中的 checkbox/radio
			_, checked := input.Attr("checked")
			if !checked {
				continue
			}
		}

		value, _ := input.Attr("value")
		result[name] = append(result[name], value)
	}
}

// extractSelectFields 提取 <select> 元素的 name/value。
func extractSelectFields(formSel *selector.Selector, result map[string][]string, userKeys map[string]bool) {
	selects := formSel.CSS("select")
	for _, sel := range selects {
		name, hasName := sel.Attr("name")
		if !hasName || name == "" || userKeys[name] {
			continue
		}

		// 查找选中的 <option>
		selectedOptions := sel.CSS("option[selected]")
		if selectedOptions.Len() > 0 {
			for _, opt := range selectedOptions {
				value, hasValue := opt.Attr("value")
				if hasValue {
					result[name] = append(result[name], value)
				} else {
					// 没有 value 属性时使用文本内容
					result[name] = append(result[name], opt.Get(""))
				}
			}
		} else {
			// 没有选中项时，使用第一个 <option> 的值（浏览器行为）
			firstOption := sel.CSS("option")
			if firstOption.Len() > 0 {
				value, hasValue := firstOption.First().Attr("value")
				if hasValue {
					result[name] = []string{value}
				} else {
					result[name] = []string{firstOption.First().Get("")}
				}
			}
		}
	}
}

// extractTextareaFields 提取 <textarea> 元素的 name/value。
func extractTextareaFields(formSel *selector.Selector, result map[string][]string, userKeys map[string]bool) {
	textareas := formSel.CSS("textarea")
	for _, ta := range textareas {
		name, hasName := ta.Attr("name")
		if !hasName || name == "" || userKeys[name] {
			continue
		}

		// <textarea> 的值是其文本内容
		value := ta.Get("")
		result[name] = []string{value}
	}
}

// addClickable 添加提交按钮的 name/value 到表单数据。
func addClickable(formSel *selector.Selector, result map[string][]string, userKeys map[string]bool, clickdata map[string]string) {
	// 查找所有可点击的提交元素
	// input[type=submit], input[type=image], button[type=submit], button（无 type 默认为 submit）
	var clickables selector.List

	submitInputs := formSel.CSS("input[type=submit], input[type=image]")
	clickables = append(clickables, submitInputs...)

	// button 元素：type=submit 或无 type 属性
	buttons := formSel.CSS("button")
	for _, btn := range buttons {
		btnType, hasType := btn.Attr("type")
		if !hasType || strings.ToLower(strings.TrimSpace(btnType)) == "submit" {
			clickables = append(clickables, btn)
		}
	}

	if len(clickables) == 0 {
		return
	}

	// 如果指定了 clickdata，查找匹配的按钮
	if clickdata != nil {
		for _, btn := range clickables {
			if matchClickdata(btn, clickdata) {
				name, hasName := btn.Attr("name")
				if hasName && name != "" && !userKeys[name] {
					value, _ := btn.Attr("value")
					result[name] = []string{value}
				}
				return
			}
		}
		// 没有找到匹配的按钮，不添加任何按钮数据
		return
	}

	// 默认使用第一个可点击元素
	first := clickables[0]
	name, hasName := first.Attr("name")
	if hasName && name != "" && !userKeys[name] {
		value, _ := first.Attr("value")
		result[name] = []string{value}
	}
}

// matchClickdata 检查按钮是否匹配 clickdata 中的所有属性。
func matchClickdata(btn *selector.Selector, clickdata map[string]string) bool {
	for attr, expected := range clickdata {
		actual, hasAttr := btn.Attr(attr)
		if !hasAttr || actual != expected {
			return false
		}
	}
	return true
}
