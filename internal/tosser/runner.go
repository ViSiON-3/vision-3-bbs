package tosser

// RunOnce performs a single import+export cycle.
func (t *Tosser) RunOnce() TossResult {
	importResult := t.ProcessInbound()
	exportResult := t.ScanAndExport()

	return TossResult{
		PacketsProcessed: importResult.PacketsProcessed,
		MessagesImported: importResult.MessagesImported,
		MessagesExported: exportResult.MessagesExported,
		DupesSkipped:     importResult.DupesSkipped,
		Errors:           append(importResult.Errors, exportResult.Errors...),
	}
}

// PurgeDupes removes old entries from the dupe database.
func (t *Tosser) PurgeDupes() error {
	return t.dupeDB.Purge()
}
