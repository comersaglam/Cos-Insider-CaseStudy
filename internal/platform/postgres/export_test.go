package postgres

// NewMigrator exposes newMigrator to the external test package, which rolls migrations back.
var NewMigrator = newMigrator
