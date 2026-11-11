package api

import (
	"encoding/json"
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

func TestListProducts(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.Get(srv.URL + "/products")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Items []store.Product `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(body.Items) != 2 || body.Items[0].ID != "a" {
		t.Fatalf("status %d, items %+v", resp.StatusCode, body.Items)
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
