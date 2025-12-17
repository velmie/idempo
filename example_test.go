package idempo_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/velmie/idempo"
	"github.com/velmie/idempo/memory"
)

func ExampleEngine() {
	store := memory.New()
	defer func() { _ = store.Close() }()

	engine := idempo.NewEngine(store)

	fp := idempo.Fingerprint{
		Operation: http.MethodPost,
		Target:    "/v1/orders",
		BodyHash:  "sha256:deadbeef",
	}

	res, err := engine.Process(context.Background(), "order:123", fp)
	if err != nil {
		panic(err)
	}
	if res.IsOwner {
		_ = engine.Commit(context.Background(), "order:123", res.Token, &idempo.Response{
			StatusCode: http.StatusCreated,
			Body:       []byte("created"),
		})
	}

	replayed, _ := engine.Process(context.Background(), "order:123", fp)
	fmt.Println(replayed.Response.StatusCode, string(replayed.Response.Body))

	// Output: 201 created
}

