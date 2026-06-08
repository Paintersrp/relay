package views

import (
	"context"
	"fmt"
	"io"
	"strconv"

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
