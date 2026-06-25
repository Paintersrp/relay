package views

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

func Unsafe(html string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := w.Write([]byte(html))
		return err
	})
}

func Itoa(i int64) string { return strconv.FormatInt(i, 10) }

func Fmt(format string, args ...interface{}) string { return fmt.Sprintf(format, args...) }

func DevReloadEnabled() bool {
	value := strings.ToLower(os.Getenv("RELAY_DEV_RELOAD"))
	return value == "1" || value == "true" || value == "yes"
}
