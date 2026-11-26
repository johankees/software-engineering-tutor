# orders

Reference project for the Go chapter of the Software Engineering Tutor: a small orders service (`ordersd`) and its command-line client (`orderctl`). Standard library only.

```sh
make help     # list all targets
make pr       # format check, vet, lint, race tests: what CI runs
make run      # start the service on :8080 with readable logs
make build    # build bin/ordersd and bin/orderctl
```

Try it:

```sh
go run ./cmd/orderctl create -customer c-1 -item BOOK-1:2:1250
go run ./cmd/orderctl list
```

The chapter pages [Building a CLI](../../docs/go/cli.md) and [Building a service](../../docs/go/services.md) explain every file.
