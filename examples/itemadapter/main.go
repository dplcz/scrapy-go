// 示例：演示 scrapy-go 的 ItemAdapter 体系完整 API。
//
// 本示例覆盖 pkg/item 包的所有核心公开函数和类型：
//   - Adapt / MustAdapt / IsItem / AsMap / FieldNames（顶层便捷函数）
//   - MapAdapter  — map[string]any / map[string]string 适配
//   - StructAdapter — struct / *struct 适配，struct tag 解析
//   - FieldMeta — Get / GetString / Clone
//   - Register — 自定义 AdapterFactory 注册
//
// 运行方式：go run examples/itemadapter/main.go
package main

import (
	"fmt"
	"strings"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// ============================================================================
// 1. 定义 struct Item（通过 struct tag 控制字段名和元数据）
// ============================================================================

// Book 演示 struct Item 的定义方式。
// - `item` tag 第一个 token 为字段名（覆盖 Go 字段名）
// - `item` tag 后续 token 为 key=value 元数据（如 serializer=strip）
// - `json` tag 作为 fallback 字段名来源
type Book struct {
	Title    string  `item:"title"`
	Author   string  `item:"author"`
	Price    float64 `item:"price,serializer=format_price"`
	InStock  bool    `item:"in_stock"`
	Tags     []string `item:"tags"`
	internal string  // 非导出字段，自动跳过
}

// Movie 演示 json tag 作为 fallback 字段名。
type Movie struct {
	Name     string `json:"name"`
	Director string `json:"director"`
	Year     int    `json:"year"`
	Rating   float64 `json:"rating,omitempty"`
}

// ============================================================================
// 2. 自定义 ItemAdapter 实现
// ============================================================================

// OrderedItem 演示用户自定义的 ItemAdapter 实现。
// 它维护插入顺序，适用于需要保持字段顺序的场景。
type OrderedItem struct {
	keys   []string
	values map[string]any
}

func NewOrderedItem() *OrderedItem {
	return &OrderedItem{values: make(map[string]any)}
}

func (o *OrderedItem) Put(key string, value any) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// 实现 item.ItemAdapter 接口
func (o *OrderedItem) Item() any                        { return o }
func (o *OrderedItem) FieldNames() []string             { return append([]string(nil), o.keys...) }
func (o *OrderedItem) GetField(name string) (any, bool) { v, ok := o.values[name]; return v, ok }
func (o *OrderedItem) SetField(name string, value any) error {
	o.Put(name, value)
	return nil
}
func (o *OrderedItem) HasField(name string) bool { _, ok := o.values[name]; return ok }
func (o *OrderedItem) AsMap() map[string]any {
	out := make(map[string]any, len(o.values))
	for k, v := range o.values {
		out[k] = v
	}
	return out
}
func (o *OrderedItem) Len() int                    { return len(o.keys) }
func (o *OrderedItem) FieldMeta(string) item.FieldMeta { return nil }

// ============================================================================
// main
// ============================================================================

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           scrapy-go ItemAdapter 完整 API 示例              ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	demoIsItem()
	demoMapAdapter()
	demoMapStringStringAdapter()
	demoStructAdapter()
	demoStructAdapterReadOnly()
	demoStructTagAndFieldMeta()
	demoJSONTagFallback()
	demoTopLevelConvenience()
	demoCustomAdapterInterface()
	demoRegisterFactory()

	fmt.Println("\n✅ 所有 ItemAdapter API 演示完成！")
}

// ============================================================================
// 演示 1：IsItem — 判断是否可适配
// ============================================================================

func demoIsItem() {
	printSection("1. IsItem — 判断类型是否可适配")

	// 可适配的类型
	fmt.Printf("  map[string]any  → IsItem = %v\n", item.IsItem(map[string]any{"a": 1}))
	fmt.Printf("  map[string]string → IsItem = %v\n", item.IsItem(map[string]string{"a": "b"}))
	fmt.Printf("  struct (Book)   → IsItem = %v\n", item.IsItem(Book{}))
	fmt.Printf("  *struct (*Book) → IsItem = %v\n", item.IsItem(&Book{}))
	fmt.Printf("  OrderedItem     → IsItem = %v\n", item.IsItem(NewOrderedItem()))

	// 不可适配的类型
	fmt.Printf("  nil             → IsItem = %v\n", item.IsItem(nil))
	fmt.Printf("  string          → IsItem = %v\n", item.IsItem("hello"))
	fmt.Printf("  int             → IsItem = %v\n", item.IsItem(42))
	fmt.Printf("  []string        → IsItem = %v\n", item.IsItem([]string{"a"}))
}

// ============================================================================
// 演示 2：MapAdapter — map[string]any 适配
// ============================================================================

func demoMapAdapter() {
	printSection("2. MapAdapter — map[string]any 适配")

	m := map[string]any{
		"title":  "Go in Action",
		"price":  39.99,
		"author": "William Kennedy",
		"tags":   []string{"go", "programming"},
	}

	// 方式一：通过 Adapt 自动检测
	adapter := item.Adapt(m)
	fmt.Printf("  Adapt(map) 类型: %T\n", adapter)

	// 方式二：直接构造 MapAdapter
	mapAdapter := item.NewMapAdapter(m)
	fmt.Printf("  NewMapAdapter 类型: %T\n", mapAdapter)

	// FieldNames — 按字典序返回
	fmt.Printf("  FieldNames(): %v\n", adapter.FieldNames())

	// GetField — 读取字段
	title, ok := adapter.GetField("title")
	fmt.Printf("  GetField(\"title\"): %v (ok=%v)\n", title, ok)

	// GetField — 不存在的字段
	_, ok = adapter.GetField("nonexistent")
	fmt.Printf("  GetField(\"nonexistent\"): ok=%v\n", ok)

	// HasField — 判断字段是否存在
	fmt.Printf("  HasField(\"price\"): %v\n", adapter.HasField("price"))
	fmt.Printf("  HasField(\"isbn\"): %v\n", adapter.HasField("isbn"))

	// SetField — 写入字段（新增或修改）
	_ = adapter.SetField("isbn", "978-1617291784")
	_ = adapter.SetField("price", 29.99) // 修改已有字段
	fmt.Printf("  SetField 后 FieldNames(): %v\n", adapter.FieldNames())
	isbn, _ := adapter.GetField("isbn")
	price, _ := adapter.GetField("price")
	fmt.Printf("  新增 isbn=%v, 修改 price=%v\n", isbn, price)

	// Len — 字段数量
	fmt.Printf("  Len(): %d\n", adapter.Len())

	// AsMap — 返回浅拷贝
	snapshot := adapter.AsMap()
	fmt.Printf("  AsMap(): %v\n", snapshot)

	// Item — 返回原始 Item
	fmt.Printf("  Item() == 原始 map: %v\n", fmt.Sprintf("%p", adapter.Item()) == fmt.Sprintf("%p", m))

	// FieldMeta — map 类型无 struct tag，返回 nil
	meta := adapter.FieldMeta("title")
	fmt.Printf("  FieldMeta(\"title\"): %v (map 无元数据)\n", meta)
}

// ============================================================================
// 演示 3：MapAdapter — map[string]string 适配
// ============================================================================

func demoMapStringStringAdapter() {
	printSection("3. MapAdapter — map[string]string 适配")

	m := map[string]string{
		"name":  "Alice",
		"email": "alice@example.com",
	}

	adapter := item.Adapt(m)
	fmt.Printf("  Adapt(map[string]string) 类型: %T\n", adapter)
	fmt.Printf("  FieldNames(): %v\n", adapter.FieldNames())

	name, ok := adapter.GetField("name")
	fmt.Printf("  GetField(\"name\"): %v (ok=%v, 类型=%T)\n", name, ok, name)

	fmt.Printf("  AsMap(): %v\n", adapter.AsMap())
}

// ============================================================================
// 演示 4：StructAdapter — 可写模式（指针传入）
// ============================================================================

func demoStructAdapter() {
	printSection("4. StructAdapter — 可写模式（*struct）")

	book := &Book{
		Title:   "The Go Programming Language",
		Author:  "Donovan & Kernighan",
		Price:   49.99,
		InStock: true,
		Tags:    []string{"go", "reference"},
	}

	// 通过 Adapt 自动检测
	adapter := item.Adapt(book)
	fmt.Printf("  Adapt(*Book) 类型: %T\n", adapter)

	// 也可以直接构造
	structAdapter := item.NewStructAdapter(book)
	fmt.Printf("  NewStructAdapter 类型: %T\n", structAdapter)

	// FieldNames — 按 struct 声明顺序返回（受 item tag 影响）
	fmt.Printf("  FieldNames(): %v\n", adapter.FieldNames())

	// GetField — 读取字段（使用 item tag 定义的名称）
	title, ok := adapter.GetField("title")
	fmt.Printf("  GetField(\"title\"): %v (ok=%v)\n", title, ok)

	price, ok := adapter.GetField("price")
	fmt.Printf("  GetField(\"price\"): %v (ok=%v)\n", price, ok)

	tags, ok := adapter.GetField("tags")
	fmt.Printf("  GetField(\"tags\"): %v (ok=%v)\n", tags, ok)

	// HasField
	fmt.Printf("  HasField(\"author\"): %v\n", adapter.HasField("author"))
	fmt.Printf("  HasField(\"internal\"): %v (非导出字段不可见)\n", adapter.HasField("internal"))

	// SetField — 可写模式（指针传入）
	err := adapter.SetField("price", 39.99)
	fmt.Printf("  SetField(\"price\", 39.99): err=%v\n", err)
	newPrice, _ := adapter.GetField("price")
	fmt.Printf("  修改后 price=%v, 原始 book.Price=%v\n", newPrice, book.Price)

	// Len
	fmt.Printf("  Len(): %d\n", adapter.Len())

	// AsMap — 返回所有字段的 map 快照
	fmt.Printf("  AsMap(): %v\n", adapter.AsMap())

	// Item — 返回原始 Item
	fmt.Printf("  Item() 类型: %T\n", adapter.Item())
}

// ============================================================================
// 演示 5：StructAdapter — 只读模式（值传入）
// ============================================================================

func demoStructAdapterReadOnly() {
	printSection("5. StructAdapter — 只读模式（struct 值传入）")

	book := Book{
		Title:  "Learning Go",
		Author: "Jon Bodner",
		Price:  42.00,
	}

	adapter := item.Adapt(book) // 值传入，非指针
	fmt.Printf("  Adapt(Book) 类型: %T\n", adapter)

	// 读取正常
	title, _ := adapter.GetField("title")
	fmt.Printf("  GetField(\"title\"): %v\n", title)

	// SetField — 只读模式，返回 ErrFieldReadOnly
	err := adapter.SetField("price", 35.00)
	fmt.Printf("  SetField(\"price\", 35.00): err=%v\n", err)
	fmt.Printf("  （值传入的 struct 不可写，需要传入指针才能修改）\n")
}

// ============================================================================
// 演示 6：FieldMeta — struct tag 元数据解析
// ============================================================================

func demoStructTagAndFieldMeta() {
	printSection("6. FieldMeta — struct tag 元数据")

	book := &Book{
		Title: "100 Go Mistakes",
		Price: 35.50,
	}

	adapter := item.Adapt(book)

	// price 字段有 serializer=format_price 元数据
	priceMeta := adapter.FieldMeta("price")
	fmt.Printf("  FieldMeta(\"price\"): %v\n", priceMeta)

	if priceMeta != nil {
		// Get — 获取任意键
		serVal, ok := priceMeta.Get("serializer")
		fmt.Printf("  priceMeta.Get(\"serializer\"): %v (ok=%v)\n", serVal, ok)

		// GetString — 获取 string 类型的值
		serStr := priceMeta.GetString("serializer")
		fmt.Printf("  priceMeta.GetString(\"serializer\"): %q\n", serStr)

		// Get 不存在的键
		_, ok = priceMeta.Get("nonexistent")
		fmt.Printf("  priceMeta.Get(\"nonexistent\"): ok=%v\n", ok)

		// Clone — 浅拷贝
		cloned := priceMeta.Clone()
		cloned["custom_key"] = "custom_value"
		fmt.Printf("  Clone 后修改不影响原始: 原始有 custom_key=%v, 克隆有 custom_key=%v\n",
			priceMeta.GetString("custom_key"), cloned.GetString("custom_key"))
	}

	// title 字段无额外元数据
	titleMeta := adapter.FieldMeta("title")
	fmt.Printf("  FieldMeta(\"title\"): %v (无额外元数据)\n", titleMeta)

	// 不存在的字段
	nilMeta := adapter.FieldMeta("nonexistent")
	fmt.Printf("  FieldMeta(\"nonexistent\"): %v\n", nilMeta)

	// nil FieldMeta 的安全操作
	var fm item.FieldMeta
	_, ok := fm.Get("key")
	fmt.Printf("  nil FieldMeta.Get(\"key\"): ok=%v (安全调用)\n", ok)
	fmt.Printf("  nil FieldMeta.GetString(\"key\"): %q\n", fm.GetString("key"))
	fmt.Printf("  nil FieldMeta.Clone(): %v\n", fm.Clone())
}

// ============================================================================
// 演示 7：json tag 作为 fallback 字段名
// ============================================================================

func demoJSONTagFallback() {
	printSection("7. json tag 作为 fallback 字段名")

	movie := &Movie{
		Name:     "Inception",
		Director: "Christopher Nolan",
		Year:     2010,
		Rating:   8.8,
	}

	adapter := item.Adapt(movie)
	fmt.Printf("  FieldNames(): %v\n", adapter.FieldNames())

	name, _ := adapter.GetField("name")
	fmt.Printf("  GetField(\"name\"): %v (来自 json tag)\n", name)

	// Rating 字段的 json tag 有 omitempty，会被记录到 FieldMeta
	ratingMeta := adapter.FieldMeta("rating")
	fmt.Printf("  FieldMeta(\"rating\"): %v\n", ratingMeta)
	if ratingMeta != nil {
		// json tag 的 omitempty 以 json_omitempty=true 形式记录
		_, hasOmit := ratingMeta.Get("json_omitempty")
		fmt.Printf("  rating 有 json_omitempty: %v\n", hasOmit)
	}

	fmt.Printf("  AsMap(): %v\n", adapter.AsMap())
}

// ============================================================================
// 演示 8：顶层便捷函数
// ============================================================================

func demoTopLevelConvenience() {
	printSection("8. 顶层便捷函数 — AsMap / FieldNames / MustAdapt")

	book := &Book{
		Title:   "Concurrency in Go",
		Author:  "Katherine Cox-Buday",
		Price:   45.00,
		InStock: true,
		Tags:    []string{"go", "concurrency"},
	}

	// item.AsMap — 便捷获取 map 快照
	m := item.AsMap(book)
	fmt.Printf("  item.AsMap(book): %v\n", m)

	// item.FieldNames — 便捷获取字段名
	names := item.FieldNames(book)
	fmt.Printf("  item.FieldNames(book): %v\n", names)

	// item.MustAdapt — 确定可适配时使用（否则 panic）
	adapter := item.MustAdapt(book)
	fmt.Printf("  item.MustAdapt(book) 类型: %T\n", adapter)

	// item.AsMap 对不可适配类型返回 nil
	nilMap := item.AsMap("not an item")
	fmt.Printf("  item.AsMap(\"not an item\"): %v\n", nilMap)

	// item.FieldNames 对不可适配类型返回 nil
	nilNames := item.FieldNames(42)
	fmt.Printf("  item.FieldNames(42): %v\n", nilNames)

	// item.Adapt 对不可适配类型返回 nil（不 panic）
	nilAdapter := item.Adapt([]int{1, 2, 3})
	fmt.Printf("  item.Adapt([]int{...}): %v\n", nilAdapter)

	// MustAdapt 对不可适配类型会 panic（此处用 recover 演示）
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("  item.MustAdapt(42) panic: %v\n", r)
			}
		}()
		item.MustAdapt(42)
	}()
}

// ============================================================================
// 演示 9：自定义 ItemAdapter 接口实现
// ============================================================================

func demoCustomAdapterInterface() {
	printSection("9. 自定义 ItemAdapter 接口实现")

	oi := NewOrderedItem()
	oi.Put("z_last", "我是最后插入但字母序最后")
	oi.Put("a_first", "我是第二个插入但字母序最前")
	oi.Put("m_middle", "我是第三个插入")

	// 实现了 ItemAdapter 接口，Adapt 直接返回自身
	adapter := item.Adapt(oi)
	fmt.Printf("  Adapt(OrderedItem) 类型: %T\n", adapter)
	fmt.Printf("  IsItem(OrderedItem): %v\n", item.IsItem(oi))

	// FieldNames 保持插入顺序（非字典序）
	fmt.Printf("  FieldNames(): %v (保持插入顺序)\n", adapter.FieldNames())

	// GetField / HasField / SetField
	val, _ := adapter.GetField("z_last")
	fmt.Printf("  GetField(\"z_last\"): %v\n", val)
	fmt.Printf("  HasField(\"a_first\"): %v\n", adapter.HasField("a_first"))

	_ = adapter.SetField("new_field", "动态新增")
	fmt.Printf("  SetField 后 FieldNames(): %v\n", adapter.FieldNames())

	// Len / AsMap
	fmt.Printf("  Len(): %d\n", adapter.Len())
	fmt.Printf("  AsMap(): %v\n", adapter.AsMap())
}

// ============================================================================
// 演示 10：Register — 自定义 AdapterFactory 注册
// ============================================================================

// CSVRow 是一个自定义类型，不是 map 也不是 struct，
// 通过注册工厂让 Adapt 能识别它。
type CSVRow struct {
	Headers []string
	Values  []string
}

// csvRowAdapter 为 CSVRow 提供 ItemAdapter 实现。
type csvRowAdapter struct {
	row *CSVRow
}

func (a *csvRowAdapter) Item() any { return a.row }
func (a *csvRowAdapter) FieldNames() []string {
	return append([]string(nil), a.row.Headers...)
}
func (a *csvRowAdapter) GetField(name string) (any, bool) {
	for i, h := range a.row.Headers {
		if h == name && i < len(a.row.Values) {
			return a.row.Values[i], true
		}
	}
	return nil, false
}
func (a *csvRowAdapter) SetField(name string, value any) error {
	for i, h := range a.row.Headers {
		if h == name && i < len(a.row.Values) {
			a.row.Values[i] = fmt.Sprintf("%v", value)
			return nil
		}
	}
	return item.ErrFieldNotFound
}
func (a *csvRowAdapter) HasField(name string) bool {
	for _, h := range a.row.Headers {
		if h == name {
			return true
		}
	}
	return false
}
func (a *csvRowAdapter) AsMap() map[string]any {
	out := make(map[string]any, len(a.row.Headers))
	for i, h := range a.row.Headers {
		if i < len(a.row.Values) {
			out[h] = a.row.Values[i]
		}
	}
	return out
}
func (a *csvRowAdapter) Len() int                    { return len(a.row.Headers) }
func (a *csvRowAdapter) FieldMeta(string) item.FieldMeta { return nil }

func demoRegisterFactory() {
	printSection("10. Register — 自定义 AdapterFactory")

	// 注册前：CSVRow 作为 *struct 会被 StructAdapter 适配（但字段名不理想）
	row := &CSVRow{
		Headers: []string{"name", "age", "city"},
		Values:  []string{"Alice", "30", "Beijing"},
	}
	beforeAdapter := item.Adapt(row)
	fmt.Printf("  注册前 Adapt(CSVRow): 类型=%T (被 StructAdapter 兜底)\n", beforeAdapter)
	fmt.Printf("  注册前 FieldNames(): %v (Go 字段名，非业务字段名)\n", beforeAdapter.FieldNames())

	// 注册自定义工厂
	item.Register(func(it any) item.ItemAdapter {
		if r, ok := it.(*CSVRow); ok {
			return &csvRowAdapter{row: r}
		}
		return nil
	})

	// 注册后：CSVRow 可以被 Adapt 识别
	adapter := item.Adapt(row)
	fmt.Printf("  注册后 Adapt(CSVRow) 类型: %T\n", adapter)
	fmt.Printf("  注册后 IsItem(CSVRow): %v\n", item.IsItem(row))
	fmt.Printf("  FieldNames(): %v\n", adapter.FieldNames())

	name, _ := adapter.GetField("name")
	fmt.Printf("  GetField(\"name\"): %v\n", name)

	_ = adapter.SetField("age", "31")
	age, _ := adapter.GetField("age")
	fmt.Printf("  SetField(\"age\", \"31\") → GetField(\"age\"): %v\n", age)

	fmt.Printf("  AsMap(): %v\n", adapter.AsMap())
}

// ============================================================================
// 辅助函数
// ============================================================================

func printSection(title string) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("─", 60))
}
