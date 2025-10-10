# Snapshot Testing

This package provides snapshot testing functionality for Go tests, allowing you to automatically generate and compare expected test outputs.

## Usage

### Basic Usage

```go
func TestMyFunction(t *testing.T) {
    snapshotTester := snapshot.New(t)
    
    // Your test logic here
    result := myFunction(input)
    
    // This will create a snapshot on first run, and compare on subsequent runs
    snapshotTester.MatchSnapshot(t, "test_name", result)
}
```

### Generating Snapshots

When you run your tests for the first time, snapshot files will be automatically created in `testdata/snapshots/`:

```bash
go test ./pkg/parser
```

This creates snapshot files like:

- `testdata/snapshots/test_name.json`

### Updating Snapshots

When your code changes and you expect different output, update the snapshots:

```bash
UPDATE_SNAPSHOTS=true go test ./pkg/parser
```

### Environment Variables

- `UPDATE_SNAPSHOTS=true`: Forces regeneration of all snapshot files
