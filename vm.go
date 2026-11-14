package main

import (
	"context"
	"net"
	"net/http"

	"charm.land/log/v2"

	httptransport "github.com/go-openapi/runtime/client"

	"clowder/client"
	"clowder/client/operations"
	"clowder/models"
)

func um() {
	socketPath := "/tmp/firecracker.socket"

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	transport := httptransport.NewWithClient(client.DefaultHost, client.DefaultBasePath, []string{"http"}, httpClient)
	apiClient := client.New(transport, nil)

	params := operations.NewPutLoggerParams().WithBody(&models.Logger{
		LogPath:       "./firecracker.log",
		Level:         new(models.LoggerLevelDebug),
		ShowLevel:     new(true),
		ShowLogOrigin: new(true),
	})

	if _, err := apiClient.Operations.PutLogger(params); err != nil {
		log.Fatal(err)
	}
}
