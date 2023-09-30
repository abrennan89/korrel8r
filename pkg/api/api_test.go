// Copyright: This file is part of korrel8r, released under https://github.com/korrel8r/korrel8r/blob/main/LICENSE

package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/korrel8r/korrel8r/internal/pkg/test"
	"github.com/korrel8r/korrel8r/internal/pkg/test/mock"
	"github.com/korrel8r/korrel8r/pkg/domains/log"
	"github.com/korrel8r/korrel8r/pkg/domains/metric"
	"github.com/korrel8r/korrel8r/pkg/engine"
	"github.com/korrel8r/korrel8r/pkg/graph"
	"github.com/korrel8r/korrel8r/pkg/korrel8r"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

func TestAPI_GetDomains(t *testing.T) {
	a := newTestAPI(mock.Domains("foo", "bar")...)
	require.NoError(t, a.Engine.AddStoreConfig(korrel8r.StoreConfig{"domain": "foo", "a": "1"}))
	require.NoError(t, a.Engine.AddStoreConfig(korrel8r.StoreConfig{"domain": "foo", "b": "2"}))
	require.NoError(t, a.Engine.AddStoreConfig(korrel8r.StoreConfig{"domain": "bar", "x": "y"}))
	assertDo(t, a, "GET", "/api/v1alpha1/domains", nil, 200, []Domain{
		{Name: "foo", Stores: []korrel8r.StoreConfig{{"a": "1", "domain": "foo"}, {"b": "2", "domain": "foo"}}},
		{Name: "bar", Stores: []korrel8r.StoreConfig{{"domain": "bar", "x": "y"}}}})
}

func TestAPI_GetDomainClasses(t *testing.T) {
	a := newTestAPI(log.Domain, metric.Domain)
	assertDo(t, a, "GET", "/api/v1alpha1/domains/log/classes", nil, 200, Classes{
		"application":    log.Application.Description(),
		"audit":          log.Audit.Description(),
		"infrastructure": log.Infrastructure.Description(),
	})
	assertDo(t, a, "GET", "/api/v1alpha1/domains/metric/classes", nil, 200, Classes{
		"metric": metric.Domain.Classes()[0].Description(),
	})
}

func TestAPI_ListGoals(t *testing.T) {
	a, x, y, _ := apiWithRules()
	assertDo(t, a, "POST", "/api/v1alpha1/lists/goals",
		GoalsRequest{
			Start: Start{
				Class:   korrel8r.ClassName(x),
				Queries: []json.RawMessage{json.RawMessage(mock.NewQuery(x, "a").String())},
				Objects: []json.RawMessage{[]byte(`"b"`)},
			},
			Goals: []string{korrel8r.ClassName(y)},
		},
		200, []Node{
			{
				Class: "y.bar",
				Count: 2,
				Queries: queryCounts(graph.Queries{
					mock.NewQuery(y, "bb").String(): 1,
					mock.NewQuery(y, "aa").String(): 1,
				}),
			},
		})
}

func TestAPI_GraphGoals_withRules(t *testing.T) {
	a, x, y, z := apiWithRules()
	yQueries := queryCounts(graph.Queries{mock.NewQuery(y, "aa").String(): 1, mock.NewQuery(y, "bb").String(): 1})
	zQueries := queryCounts(graph.Queries{mock.NewQuery(z, "c").String(): 1})
	xQuery := mock.NewQuery(x, "a").String()
	assertDo(t, a, "POST", "/api/v1alpha1/graphs/goals?withRules=true",
		GoalsRequest{
			Start: Start{
				Class:   korrel8r.ClassName(x),
				Queries: []json.RawMessage{json.RawMessage(xQuery)},
				Objects: []json.RawMessage{[]byte(`"b"`)},
			},
			Goals: []string{korrel8r.ClassName(z)},
		},
		200,
		Graph{
			Nodes: []Node{
				{Class: "x.foo", Count: 2, Queries: queryCounts(graph.Queries{xQuery: 1})},
				{Class: "y.bar", Count: 2, Queries: yQueries},
				{Class: "z.bar", Count: 1, Queries: zQueries},
			},
			Edges: []Edge{
				{Start: "x.foo", Goal: "y.bar", Rules: []Rule{{Name: "x->y", Queries: yQueries}}},
				{Start: "y.bar", Goal: "z.bar", Rules: []Rule{{Name: "y->z", Queries: zQueries}}},
			},
		})
}

func TestAPI_PostNeighbours_noRules(t *testing.T) {
	a, x, y, _ := apiWithRules()
	yQueries := queryCounts(graph.Queries{mock.NewQuery(y, "aa").String(): 1})
	assertDo(t, a, "POST", "/api/v1alpha1/graphs/neighbours",
		NeighboursRequest{
			Start: Start{
				Class:   korrel8r.ClassName(x),
				Objects: []json.RawMessage{[]byte(`"a"`)},
			},
			Depth: 1,
		},
		200,
		Graph{
			Nodes: []Node{
				{Class: "x.foo", Count: 1},
				{Class: "y.bar", Count: 1, Queries: yQueries},
			},
			Edges: []Edge{
				{Start: "x.foo", Goal: "y.bar"},
			},
		},
	)
}

func ginEngine() *gin.Engine {
	if os.Getenv(gin.EnvGinMode) == "" { // Don't override an explicit env setting.
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	if flag.Lookup("test.v") != nil {
		r.Use(gin.Logger())
	}
	return r
}

type testAPI struct {
	*API
	Router *gin.Engine
}

func newTestAPI(domains ...korrel8r.Domain) *testAPI {
	r := ginEngine()
	return &testAPI{API: test.Must(New(engine.New(domains...), r)), Router: r}
}

func do(t *testing.T, a *testAPI, method, url string, body any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var r io.Reader
	if body != nil {
		r = strings.NewReader(test.JSONString(body))
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		w.Code = http.StatusBadRequest
		fmt.Fprintln(w, err.Error())
	} else {
		a.Router.ServeHTTP(w, req)
	}
	return w
}

// normalize values by sorting slices to avoid test failure due to ordering inconsistency.
func normalize(v any) {
	switch v := v.(type) {
	case Graph:
		slices.SortFunc(v.Nodes, func(a, b Node) int { return strings.Compare(a.Class, b.Class) })
		for _, n := range v.Nodes {
			slices.SortFunc(n.Queries, func(a, b QueryCount) int { return bytes.Compare(a.Query, b.Query) })
		}
		slices.SortFunc(v.Edges, Edge.Compare)
		for _, e := range v.Edges {
			for _, r := range e.Rules {
				slices.SortFunc(r.Queries, func(a, b QueryCount) int { return bytes.Compare(a.Query, b.Query) })
			}
		}
	}
}

func assertDo[T any](t *testing.T, a *testAPI, method, url string, req any, code int, want T) {
	t.Helper()
	w := do(t, a, method, url, req)
	if assert.Equal(t, code, w.Code, w.Body.String()) {
		var got T
		if assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), "body: %v", w.Body.String()) {
			normalize(want)
			normalize(got)
			if assert.Equal(t, test.JSONString(want), test.JSONString(got)) {
				return
			}
		}
	}
	t.Logf("request: %v", test.JSONString(req)) // Log the request body on error.
}

// doubleFunc returns a goal object with the name of the start object repeated twice.
func doubleFunc(goal korrel8r.Class) func(korrel8r.Object, *korrel8r.Constraint) (korrel8r.Query, error) {
	return func(o korrel8r.Object, _ *korrel8r.Constraint) (korrel8r.Query, error) {
		return mock.NewQuery(goal, o.(string)+o.(string)), nil
	}
}

func apiWithRules() (a *testAPI, x, y, z korrel8r.Class) {
	foo, bar := mock.Domain("foo"), mock.Domain("bar")
	api := newTestAPI(foo, bar)
	x, y, z = foo.Class("x"), bar.Class("y"), bar.Class("z")
	test.PanicErr(api.Engine.AddStore(mock.NewStore(foo)))
	test.PanicErr(api.Engine.AddStore(mock.NewStore(bar)))
	api.Engine.AddRules(mock.NewApplyRule("x->y", x, y, doubleFunc(y)))
	api.Engine.AddRules(mock.NewQueryRule("y->z", y, mock.NewQuery(z, "c")))
	return api, x, y, z
}
