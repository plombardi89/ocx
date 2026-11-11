package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/shop/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	s := store.New()
	for _, p := range []store.Product{
		{ID: "a", Name: "Apple", Price: 50, Stock: 10},
		{ID: "b", Name: "Banana", Price: 25, Stock: 0},
	} {
		if err := s.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(New(s))
	t.Cleanup(srv.Close)
	return srv, s
}

func addProducts(t *testing.T, s *store.Store, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := s.Put(store.Product{ID: fmt.Sprintf("p%03d", i), Name: "x"}); err != nil {
			t.Fatal(err)
		}
	}
}

type listResponse struct {
	Items []store.Product `json:"items"`
	Total int             `json:"total"`
}

func getList(t *testing.T, url string) (int, listResponse) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body listResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body
}

func TestListProducts(t *testing.T) {
	srv, _ := newServer(t)
	status, body := getList(t, srv.URL+"/products")
	if status != http.StatusOK || len(body.Items) != 2 || body.Items[0].ID != "a" || body.Total != 2 {
		t.Fatalf("status %d, body %+v", status, body)
	}
}

func TestListPagination(t *testing.T) {
	srv, s := newServer(t)
	for _, id := range []string{"c", "d", "e"} {
		if err := s.Put(store.Product{ID: id, Name: id, Price: 1}); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		query string
		ids   string
	}{
		{"?limit=2", "a,b"},
		{"?offset=2&limit=2", "c,d"},
		{"?offset=4", "e"},
	}
	for _, c := range cases {
		status, body := getList(t, srv.URL+"/products"+c.query)
		var ids []string
		for _, p := range body.Items {
			ids = append(ids, p.ID)
		}
		if status != http.StatusOK || strings.Join(ids, ",") != c.ids || body.Total != 5 {
			t.Errorf("%s: status %d, ids %v, total %d; want %s of 5", c.query, status, ids, body.Total, c.ids)
		}
	}
}

func TestListDefaultLimit(t *testing.T) {
	srv, s := newServer(t)
	addProducts(t, s, 23)
	status, body := getList(t, srv.URL+"/products")
	if status != http.StatusOK || len(body.Items) != 20 || body.Total != 25 {
		t.Fatalf("status %d, %d items of %d; want 20 of 25", status, len(body.Items), body.Total)
	}
}

func TestListLimitCap(t *testing.T) {
	srv, _ := newServer(t)
	status, body := getList(t, srv.URL+"/products?limit=500")
	if status != http.StatusOK || len(body.Items) > 100 || body.Total != 2 {
		t.Fatalf("status %d, %d items of %d; want at most 100", status, len(body.Items), body.Total)
	}
}

func TestListPastEndReturnsEmptyItems(t *testing.T) {
	srv, _ := newServer(t)
	for _, offset := range []string{"2"} {
		resp, err := http.Get(srv.URL + "/products?offset=" + offset)
		if err != nil {
			t.Fatalf("offset=%s: %v", offset, err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("offset=%s: %v", offset, err)
		}
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"items":[]`) || !strings.Contains(string(raw), `"total":2`) {
			t.Errorf("offset=%s: status %d, body %s; want 200 with empty items of 2", offset, resp.StatusCode, raw)
		}
	}
}

func TestListInvalidParameters(t *testing.T) {
	srv, _ := newServer(t)
	for _, q := range []string{"?limit=abc", "?offset=-1", "?limit=-5", "?offset=1.5", "?limit=", "?offset=", "?limit=abc;", "?offset=%zz", "?limit=1;2"} {
		resp, err := http.Get(srv.URL + "/products" + q)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Error string `json:"error"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || decodeErr != nil || body.Error == "" {
			t.Errorf("%s: status %d, error %q (%v); want 400 with a JSON error", q, resp.StatusCode, body.Error, decodeErr)
		}
	}
}

func TestGetProduct(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.Get(srv.URL + "/products/a")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p store.Product
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || p.Name != "Apple" {
		t.Fatalf("status %d, product %+v", resp.StatusCode, p)
	}

	missing, err := http.Get(srv.URL + "/products/zzz")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing product: status %d, want 404", missing.StatusCode)
	}
}

func TestPutProduct(t *testing.T) {
	srv, s := newServer(t)
	cases := []struct {
		body string
		want int
	}{
		{`{"id":"c","name":"Cherry","price_cents":300,"stock":5}`, http.StatusCreated},
		{`{"id":"d","name":"Bad","price_cents":-1,"stock":5}`, http.StatusBadRequest},
		{`{"id":"e","name":"Extra","price_cents":1,"stock":1,"color":"red"}`, http.StatusBadRequest},
		{`not json`, http.StatusBadRequest},
	}
	for _, c := range cases {
		resp, err := http.Post(srv.URL+"/products", "application/json", strings.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("POST %s: status %d, want %d", c.body, resp.StatusCode, c.want)
		}
	}
	if _, ok := s.Get("c"); !ok {
		t.Fatal("created product not stored")
	}
}
