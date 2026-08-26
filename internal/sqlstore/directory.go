package sqlstore

import "os"

func ensureDirectory(path string) error { return os.MkdirAll(path, 0o755) }
