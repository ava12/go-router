package router

import (
	"testing"
	"net/http"
	"strconv"
	"net/url"
)


type mockHandler struct {
	t *testing.T
	Id string
}

var expectedHandler *mockHandler

func (mh *mockHandler) ServeEx (c *Context) {
	if expectedHandler.Id != mh.Id {
		mh.t.Errorf("%s %s: expecting \"%s\" handler, got \"%s\"", c.Request.Method, c.Request.URL.String(), expectedHandler.Id, mh.Id)
	}
}


type mockWriter bool

func (*mockWriter) Header () http.Header {
	return http.Header {}
}

func (*mockWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (*mockWriter) WriteHeader(statusCode int) {}


func TestMethodRouter (t *testing.T) {
	var (
		mw mockWriter
		mr http.Request
	)

	url, _ := url.Parse("example.com")
	mr.URL = url

	hGet := &mockHandler {t, "get"}
	hPost := &mockHandler {t, "post"}
	hHead := &mockHandler {t, "head"}
	hDefault := &mockHandler {t, "default"}

	r := NewMethodRouter(hDefault)
	r.AddGet(hGet)
	r.AddPost(hPost)

	cases := []struct{m string; h *mockHandler} {
		{http.MethodDelete, hDefault},
		{http.MethodPost, hPost},
		{http.MethodHead, hGet},
		{http.MethodGet, hGet},
		{http.MethodDelete, hDefault},
	}

	env := make(map[string]string)

	for _, c := range cases {
		mr.Method = c.m
		expectedHandler = c.h
		r.ServeEx(&Context {&mw, &mr, env})
	}

	r.Add(http.MethodHead, hHead)
	mr.Method = http.MethodHead
	expectedHandler = hHead
	r.ServeEx(&Context {&mw, &mr, env})
}


func runPathSpecs (t *testing.T, cases [][]string, areValid bool) {
	h := &mockHandler{t, ""}
	mustFail := !areValid

	for index, paths := range cases {
		pr := NewPathRouter(h)
		lastPathIndex := len(paths) - 1

		for i, path := range paths {
			e := pr.Add(path, h)
			if (e != nil) == (mustFail && i == lastPathIndex) {
				continue
			}

			message := "case " + strconv.Itoa(index) + " (" + path + "): "
			if e == nil {
				message += "- no error -"
			} else {
				message += ": " + e.Error()
			}
			t.Error(message)
			break
		}
	}
}

func TestValidPathSpecs (t *testing.T) {
	cases := [][]string {
		{""},
		{"*"},
		{"/*"},
		{"foo/$bar"},
		{"foo/#bar"},
		{"foo/$bar/*"},
		{"foo/#bar/*"},
		{"foo/$bar/baz"},
		{"foo/#bar/baz"},
		{"$foo/$bar/"},
		{"#foo/#bar/"},
		{"/*", "foo", "/static/*", "/static/config"},
		{"user/new", "user/$action", "user/#id"},
		{"user/$action", "user/$action/#id"},
		{"user/#id", "user/#id/$action"},
	}

	runPathSpecs(t, cases, true)
}

func TestInvalidPathSpecs (t *testing.T) {
	cases := [][]string {
		{"/foo/*/*"},
		{"/foo/$"},
		{"/foo/#"},
		{"foo", "foo/*"},
		{"foo", "foo"},
		{"user/$action", "user/$op/#id"},
		{"user/#uid", "user/#id/$op"},
	}

	runPathSpecs(t, cases, false)
}

type (
	pathSpec struct {
		Path string
		Id string
	}

	mp map[string]string

	pathMatch struct {
		Url string
		Expected string
		Params mp
	}
)

func TestPathRouter (t *testing.T) {
	pr := NewPathRouter(&mockHandler{t, "default"})
	v := make(mp, 0)

	specs := []pathSpec {
		{"", "root"},
		{"files/*", "file"},
		{"files/config.js", "config"},
		{"user/#uid", "profile"},
		{"user/#uid/avatar", "avatar"},
		{"user/#uid/avatar/orig", "orig"},
		{"user/$action/*", "action"},
		{"user/#uid/$action", "id-action"},
		{"foo/*", "foo"},
		{"foo/$bar/baz", "foobar"},
	}

	matches := []pathMatch {
		{"/invalid/path", "default", v},
		{"/", "root", v},
		{"/files", "file", v},
		{"/files/index.html", "file", mp {"*": "index.html"}},
		{"/files/css/style.css", "file", mp {"*": "css/style.css"}},
		{"/files/Foo.txt?t=123", "file", mp {"*": "Foo.txt"}},
		{"/files/config.js", "config", v},
		{"/user", "default", v},
		{"/user/123", "profile", mp {"uid": "123"}},
		{"/User/123", "default", v},
		{"/user/12/avatar", "avatar", mp {"uid": "12"}},
		{"/user/12/avatar/orig", "orig", mp {"uid": "12"}},
		{"/user/0", "action", mp {"action": "0"}},
		{"/user/1a", "action", mp {"action": "1a"}},
		{"/user/list", "action", mp {"action": "list"}},
		{"/user/list/all", "action", mp {"action": "list", "*": "all"}},
		{"/user/123/message", "id-action", mp {"uid": "123", "action": "message"}},
		{"/foo/123/zab", "foo", mp {"bar": ""}},
	}

	var mw mockWriter
	expectedHandler = &mockHandler {}
	for _, spec := range specs {
		e := pr.Add(spec.Path, &mockHandler {t, spec.Id})
		if e != nil {
			t.Error(e.Error())
			return
		}
	}

	for _, match := range matches {
		request := http.Request {}
		Url, e := url.Parse(match.Url)
		if e != nil {
			t.Error(e.Error())
			continue
		}

		request.URL = Url
		expectedHandler.Id = match.Expected
		c := &Context {&mw, &request, make(map[string]string)}
		pr.ServeEx(c)

		if match.Params != nil && len(match.Params) > 0 {
			for name, value := range match.Params {
				got := c.Env[name]
				if got != value {
					t.Errorf("%s, expected %s=%s, got %v", match.Url, name, value, got)
				}
			}
		}
	}
}

