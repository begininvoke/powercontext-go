package sqlstore

import (
	"net"
	"testing"

	embeddedseekdb "github.com/ob-labs/powercontext-go/internal/seekdb"
)

func TestSeekDBDriverConfigUsesLocalUnixSocket(t *testing.T) {
	t.Parallel()
	config, err := seekDBDriverConfig(embeddedseekdb.ConnectionOptions{
		Transport: "unix_socket", Endpoint: "/tmp/seekdb.sock", User: "root",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if config.Net != "unix" || config.Addr != "/tmp/seekdb.sock" || config.User != "root" ||
		config.DBName != "test" || config.Params["autocommit"] != "0" || !config.ParseTime {
		t.Fatalf("driver config = %#v", config)
	}
}

func TestSeekDBDriverConfigUsesLoopbackForTCP(t *testing.T) {
	t.Parallel()
	config, err := seekDBDriverConfig(embeddedseekdb.ConnectionOptions{
		Transport: "tcp", Port: 2881, User: "root",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if config.Net != "tcp" || config.Addr != net.JoinHostPort("localhost", "2881") {
		t.Fatalf("driver endpoint = %s/%s", config.Net, config.Addr)
	}
}

func TestSeekDBDriverConfigRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		options  embeddedseekdb.ConnectionOptions
		database string
	}{
		{name: "database", options: embeddedseekdb.ConnectionOptions{Transport: "tcp", Port: 2881, User: "root"}, database: "custom"},
		{name: "user", options: embeddedseekdb.ConnectionOptions{Transport: "tcp", Port: 2881}, database: "test"},
		{name: "transport", options: embeddedseekdb.ConnectionOptions{Transport: "pipe", User: "root"}, database: "test"},
		{name: "port", options: embeddedseekdb.ConnectionOptions{Transport: "tcp", User: "root"}, database: "test"},
		{name: "socket", options: embeddedseekdb.ConnectionOptions{Transport: "unix_socket", User: "root"}, database: "test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := seekDBDriverConfig(test.options, test.database); err == nil {
				t.Fatal("invalid seekDB options were accepted")
			}
		})
	}
}
