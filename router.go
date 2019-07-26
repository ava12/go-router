// Пакет router предоставляет интерфейс для маршрутизации HTTP-запросов.
// Реализованы два маршрутизатора: по имени HTTP-метода и по пути URL.
package router

import (
	"context"
	"net/http"
	"strings"
	"strconv"
)

// RouteSpecError - тип ошибки для некорректно заданного маршрута.
type RouteSpecError struct {m string}

func (e *RouteSpecError) Error () string {
	return e.m
}


// MethodRouter маршрутизирует запросы по названию HTTP-метода.
// Для метода HEAD если соответствующий обработчик отсутствует, то вызывается
// обработчик метода GET.
// Если обработчика для требуемого метода нет, то вызывается обрабочик по умолчанию.
type MethodRouter struct {
	// handlers - список обрабочиков.
	// Поскольку число возможных HTTP-методов невелико, для поиска обработчика
	// используется простой перебор.
	handlers []*methodHandler

	// defaultHandler - обработчик по умолчанию (не nil!).
	defaultHandler http.Handler
}

// methodHandler - структура, хранящая обработчик для определенного HTTP-метода.
type methodHandler struct {
	// handler содержит обработчик для требуемого метода.
	handler http.Handler

	// method содержит имя HTTP-метода.
	method string
}

// NewMethodRouter() создает маршрутизатор по HTTP-методу.
// Если defaultHandler не задан, то используется обертка для net/http.NotFoundHandler().
func NewMethodRouter (defaultHandler http.Handler) *MethodRouter {
	if defaultHandler == nil {
		defaultHandler = http.NotFoundHandler()
	}
	return &MethodRouter {handlers: make([]*methodHandler, 0, 2), defaultHandler: defaultHandler}
}

// find() возвращает обработчик для указанного метода либо nil, если обработчик не задан.
func (mr *MethodRouter) find (method string) *methodHandler {
	for _, mh := range mr.handlers {
		if mh.method == method {
			return mh
		}
	}

	return nil
}

// Add() добавляет обработчик для указанного метода.
// Возвращает ошибку, если обработчик уже задан.
func (mr *MethodRouter) Add (method string, handler http.Handler) error {
	mh := mr.find(method)
	if mh != nil {
		return &RouteSpecError {"handler already set"}
	}

	mr.handlers = append(mr.handlers, &methodHandler {handler, method})
	return nil
}

// AddGet() добавляет/заменяет обработчик для метода GET.
func (mr *MethodRouter) AddGet (handler http.Handler) {
	mr.Add(http.MethodGet, handler)
}

// AddPost() добавляет/заменяет обработчик для метода POST.
func (mr *MethodRouter) AddPost (handler http.Handler) {
	mr.Add(http.MethodPost, handler)
}

// ServeHTTP() ищет и вызывает обработчик для текущего метода (либо обработчик по умолчанию).
func (mr *MethodRouter) ServeHTTP (w http.ResponseWriter, r *http.Request) {
	var handler http.Handler
	mh := mr.find(r.Method)
	if mh == nil && r.Method == http.MethodHead {
		mh = mr.find(http.MethodGet)
	}
	if mh != nil {
		handler = mh.handler
	} else {
		handler = mr.defaultHandler
	}
	handler.ServeHTTP(w, r)
}


// Первые символы для специальных компонентов пути.
const (
	pathOptTail     = '*' // необязательный "хвост" пути
	pathParamPrefix = '$' // префикс имени строкового параметра
	pathIndexPrefix = '#' // префикс имени положителного целочисленного параметра
)

// Типы/приоритеты (меньше - выше) параметров пути.
const (
	paramIndexType  = 2 // целочисленный
	paramStringType = 3 // строковый
)

// pathPartNode - узел дерева элементов заданных путей.
type pathPartNode struct {
	// indexTree - двоичное дерево поиска для точных значений элемента пути;
	// nil, если точные значения для данного узла не заданы.
	indexTree *pathIndexNode

	// paramNode - односвязный список параметров,
	// упорядочен по приоритету типов (высший приоритет в начале списка),
	// каждый тип параметра встречается не более одного раза;
	// nil, если параметры для данного узла не заданы.
	paramNode *pathParamNode

	// handler - обработчик для данного узла;
	// nil, если узел не может быть конечной точкой в разборе пути.
	handler http.Handler

	// tailAllowed - true, если узел может быть конечной точкой разбора,
	// и путь может содержать "хвост"; иначе false.
	tailAllowed bool
}

// pathParamNode - элемент списка возможных параметров для определенного элемента пути.
type pathParamNode struct {
	// Name - имя параметра.
	Name string

	// NextParam - следующий параметр (с более низким приоритетом) либо nil.
	NextParam *pathParamNode

	// NextPart - узел в дереве путей для данного параметра.
	NextPart *pathPartNode

	// Type - тип/приоритет параметра.
	Type int
}

// pathIndexNode - узел дерева поиска для точных значений элемента пути.
type pathIndexNode struct {
	// Key - значение элемента пути.
	Key string

	// NextPart - узел дерева путей для данного значения.
	NextPart *pathPartNode

	// leftChild - левый потомок (для ключей меньше текущего), nil, если отсутствует.
	leftChild *pathIndexNode

	// rightChild - правый потомок (для ключей больше текущего), nil, если отсутствует.
	rightChild *pathIndexNode
}

// AddPart() добавляет (при необходимости) в дерево путей и возвращает следующий элемент (точное значение или параметр).
// Возвращает ошибку, если в списке уже есть параметр с указанным типом, но с другим именем.
func (pn *pathPartNode) AddPart (part string) (*pathPartNode, error) {
	if part == "" {
		return nil, &RouteSpecError {"empty path component"}
	}

	firstChar := part[0]
	switch firstChar {
		case pathOptTail:
			return nil, &RouteSpecError {"incorrect path component"}

		case pathParamPrefix, pathIndexPrefix:
			var t int
			if firstChar == pathIndexPrefix {
				t = paramIndexType
			} else {
				t = paramStringType
			}
			name := part[1:]
			if name == "" {
				return nil, &RouteSpecError {"empty parameter name"}
			}

			return pn.addParam(name, t)

		default:
			return pn.addLiteral(part), nil
	}
}

// addLiteral() добавляет в дерево путей и возвращает следующий узел для точного значения элемента пути.
func (pn *pathPartNode) addLiteral (key string) *pathPartNode {
	if pn.indexTree != nil {
		return pn.indexTree.Add(key)
	}

	result := &pathPartNode {}
	pn.indexTree = &pathIndexNode {Key: key, NextPart: result}
	return result
}

// addParam() добавляет в дерево путей и возвращает следующий узел для элемента-параметра указанного типа.
// Возвращает ошибку, если в списке уже есть параметр с указанным типом, но с другим именем.
func (pn *pathPartNode) addParam (name string, typ int) (*pathPartNode, error) {
	if pn.paramNode == nil {
		result := &pathPartNode {}
		pn.paramNode = &pathParamNode {Name: name, Type: typ, NextPart: result}
		return result, nil
	}

	var parentNode *pathParamNode
	currentNode := pn.paramNode
	for currentNode != nil && currentNode.Type <= typ {
		if currentNode.Type == typ {
			if currentNode.Name == name {
				return currentNode.NextPart, nil
			}

			return nil, &RouteSpecError {"cannot add \"" + name + "\" parameter: \"" + currentNode.Name + "\" is already used"}
		}

		parentNode = currentNode
		currentNode = currentNode.NextParam
	}

	result := &pathPartNode {}
	paramNode := &pathParamNode {Name: name, Type: typ, NextPart: result, NextParam: currentNode}
	if parentNode != nil {
		parentNode.NextParam = paramNode
	} else {
		pn.paramNode = paramNode
	}

	return result, nil
}

// SetHandler() задает обработчик для данного узла.
// Возвращает ошибку, если обработчик уже задан.
func (pn *pathPartNode) SetHandler (handler http.Handler, tailAllowed bool) error {
	if pn.handler != nil {
		return &RouteSpecError {"handler already set"}
	}

	pn.handler = handler
	pn.tailAllowed = tailAllowed
	return nil
}

// Match() возвращает следующий узел дерева путей и имя параметра
// (пустую строку, если параметр не сопоставлен) для заданного элемента URL-пути.
// Возвращает nil и пустую строку, если соответствие не найдено.
func (pn *pathPartNode) Match (value string) (node *pathPartNode, paramName string) {
	if pn.indexTree != nil {
		result := pn.indexTree.Match(value)
		if result != nil {
			return result, ""
		}
	}

	return pn.matchParam(value)
}

// matchParam() следующий узел дерева путей и имя параметра для заданного элемента URL-пути.
// Возвращает nil и пустую строку, если соответствие не найдено.
func (pn *pathPartNode) matchParam (value string) (node *pathPartNode, paramName string) {
	paramNode := pn.paramNode

loop:
	for paramNode != nil {
		switch paramNode.Type {
			case paramIndexType:
				i, e := strconv.Atoi(value)
				if e == nil && i > 0 {
					break loop
				}

			default:
				break loop
		}

		paramNode = paramNode.NextParam
	}

	if paramNode != nil {
		return paramNode.NextPart, paramNode.Name

	} else {
		return nil, ""
	}
}

// GetHandler() возвращает обработчик для данного узла, если узел может быть
// конечной точкой разбора и либо URL-путь разобран полностью (hasTail = false),
// либо узел допускает наличие "хвоста".
// Возвращает nil в противном случае.
func (pn *pathPartNode) GetHandler (hasTail bool) http.Handler {
	if hasTail && !pn.tailAllowed {
		return nil

	} else {
		return pn.handler
	}
}

// traverse() обходит дерево поиска и возвращает узел дерева путей,
// соответствующий указанному значению элемента пути.
// Если нужный узел отсутствует, то функция либо возвращает
// nil, если allocate = false, либо
// добавляет новые узлы в дерево поиска и дерево путей и возвращает
// новый узел дерева путей, если allocate = true.
func (in *pathIndexNode) traverse (key string, allocate bool) *pathPartNode {
	var nextNode *pathIndexNode
	node := in

	for {
		if key == node.Key {
			return node.NextPart
		}

		if key < node.Key {
			nextNode = node.leftChild
		} else {
			nextNode = node.rightChild
		}

		if nextNode != nil {
			node = nextNode
		} else {
			break
		}
	}

	if !allocate {
		return nil
	}

	nextNode = &pathIndexNode {Key: key, NextPart: &pathPartNode {}}
	if key < node.Key {
		node.leftChild = nextNode
	} else {
		node.rightChild = nextNode
	}
	return nextNode.NextPart
}

// Add() добавляет в индекс указанное значение и возвращает
// соответствующий узел дерева путей.
func (in *pathIndexNode) Add (key string) *pathPartNode {
	return in.traverse(key, true)
}

// Match() ищет в индексе указанное значение и возвращает
// соответствующий узел дерева путей либо
// nil, если значение в индексе отсутствует.
func (in *pathIndexNode) Match (key string) *pathPartNode {
	return in.traverse(key, false)
}

/*
PathRouter маршрутизирует запросы по URL-пути.

Все задаваемые пути регистрозависимы, т. ч. "/foo" и "/Foo" - это разные пути.
Начальный и конечный символ "/" и в задаваемых путях, и в URL-ах игнорируются,
т. ч. "foo", "/foo", "foo/" и "/foo/" - один и тот же путь.

Элементы задаваемых путей помимо точных значений могут содержать специальные последовательности:
 - $имя - строковый параметр;
 - #имя - положительный целочисленный параметр;
 - * - необязательный "хвост" пути (может быть только последним элементом).

Все параметры, захваченные при разборе пути, добавляются в контекст
HTTP-запроса, под соответствующими именами.
Если имеется неразобранный "хвост" пути, то он записывается в параметр "*".

Маршрутизатор, разумеется, пытается использовать самый длинный путь разбора
(длина "хвоста" не учитывается).

В одной и той же точке пути могут присутствовать и точные значения, и параметры обоих типов.
Например, можно зарегистрировать пути "user/1", "user/#id" и "user/$action".
В таком случае будет выбран подходящий вариант рабора с самым высоким приоритетом
(высший приоритет у точных значений, низший у строковых параметров). В данном примере
URL "/user/1" будет распознан как первый путь, "/user/123" как второй,
"/user/profile" как третий.

В одной и той же точке пути не могут присутствовать параметры одного типа,
но с разными именами. Например, сочетание путей "user/#id" и "user/#uid/$action"
недопустимо.

Тип элемента пути при разборе назначается только один раз. Если в результате путь
не может быть распознан, то возврата назад и проверки альтернативных типов не будет.
Например, если заданы пути "user/profile" и "user/$action/#id",
то URL "/user/profile/123" не будет распознан, и будет вызван обработчик по умолчанию.

*/
type PathRouter struct {
	// defaultHandler - обработчик по умолчанию (не nil!).
	defaultHandler http.Handler

	// pathTree - дерево заданных путей, изначально nil.
	// Корень соответствует корневому URL-пути ("/").
	pathTree *pathPartNode
}

// NewPathRouter() создает маршрутизатор по URL-пути.
// Если defaultHandler не задан, то используется обертка для net/http.NotFoundHandler().
func NewPathRouter (defaultHandler http.Handler) *PathRouter {
	if defaultHandler == nil {
		defaultHandler = http.NotFoundHandler()
	}
	return &PathRouter {defaultHandler, &pathPartNode {}}
}

// Add() добавляет обработчик для указанного пути.
// Возвращает ошибку, если путь задан некорректно либо обработчик для пути уже задан.
func (pr *PathRouter) Add (pathString string, handler http.Handler) error {
	var (e error; tailAllowed bool)

	pathString = strings.Trim(pathString, "/")
	path := strings.Split(pathString, "/")
	lastPart := path[len(path) - 1]
	if lastPart != "" {
		tailAllowed = (lastPart[0] == pathOptTail)
		if tailAllowed {
			path = path[:len(path) - 1]
		}
	}

	if (len(path) == 1 && path[0] == "") || len(path) == 0 {
		pr.pathTree.SetHandler(handler, tailAllowed)
		return nil
	}

	node := pr.pathTree
	for _, name := range path {
		node, e = node.AddPart(name)
		if e != nil {
			return e
		}
	}

	return node.SetHandler(handler, tailAllowed)
}

type paramEntry struct {
	name, value string
}

// ServeHTTP() ищет и вызывает обработчик для текущего пути (либо обработчик по умолчанию).
func (pr *PathRouter) ServeHTTP (w http.ResponseWriter, r *http.Request) {
	if pr.pathTree == nil {
		pr.defaultHandler.ServeHTTP(w, r)
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if path[0] == "" {
		path = []string {}
	}
	params := []paramEntry {}
	lastPartIndex := len(path) - 1

	matchedHandler := pr.defaultHandler
	matchedParamCnt := 0
	matchedPos := 0

	node := pr.pathTree
	handler := node.GetHandler(lastPartIndex >= 0)
	if handler != nil {
		matchedHandler = handler
	}

	for i, part := range path {
		nextNode, paramName := node.Match(part)

		if nextNode == nil {
			break
		}

		if paramName != "" {
			params = append(params, paramEntry {paramName, part})
		}

		node = nextNode
		handler = node.GetHandler(i < lastPartIndex)
		if handler == nil {
			continue
		}

		matchedPos = i
		matchedHandler = handler
		matchedParamCnt = len(params)
	}

	ctx := r.Context()
	if matchedParamCnt > 0 {
		for _, entry := range params[:matchedParamCnt] {
			ctx = context.WithValue(ctx, entry.name, entry.value)
		}
	}

	if matchedPos < lastPartIndex {
		ctx = context.WithValue(ctx, "*", strings.Join(path[matchedPos + 1:], "/"))
	}

	if matchedParamCnt > 0 || matchedPos < lastPartIndex {
		r = r.WithContext(ctx)
	}

	matchedHandler.ServeHTTP(w, r)
}
