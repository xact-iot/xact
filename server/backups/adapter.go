package backups

import (
	"context"
	"io"
)

type Adapter interface {
	ExportSchema(ctx context.Context) (*Schema, error)

	ListTables(ctx context.Context) ([]string, error)

	ExportTable(ctx context.Context, table string, w io.Writer) error

	CreateTable(ctx context.Context, name string, table Table) error

	ImportTable(ctx context.Context, name string, table Table, r io.Reader) error

	FinalizeTable(ctx context.Context, name string, table Table) error
}
