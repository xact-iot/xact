package backups

func nullableColumns(table Table) map[string]bool {
	nullable := make(map[string]bool, len(table.Columns))
	for _, column := range table.Columns {
		if column.Nullable {
			nullable[column.Name] = true
		}
	}
	return nullable
}
