# `gt` verification map

Expected user-facing behavior per command. The runner is the integration
suite, not a shell harness:

```sh
go test -tags=integration -timeout 15m ./test/integration/
```

Those tests close stdin. They will not open the checkout picker, a
browser, or `gh stack switch`. Picker layout is unit-tested in
`cmd/gt/forest_test.go` and `cmd/gt/picker_test.go`.

Read [gaps.md](./gaps.md) before claiming a TUI or GitHub path was
verified.

## Features

- [create](./create.md)
- [modify](./modify.md)
- [track](./track.md)
- [log](./log.md)
- [up](./up.md)
- [down](./down.md)
- [top](./top.md)
- [bottom](./bottom.md)
- [trunk](./trunk.md)
- [checkout](./checkout.md)
- [delete](./delete.md)
- [restack](./restack.md)
- [sync](./sync.md)
- [doctor](./doctor.md)
- [continue](./continue.md)
- [abort](./abort.md)
- [add](./add.md)
- [restore](./restore.md)
- [reset](./reset.md)
- [rebase](./rebase.md)
- [cherry-pick](./cherry-pick.md)
- [init](./init.md)
- [help](./help.md)
- [unsupported](./unsupported.md)
- [unknown](./unknown.md)
- [submit](./submit.md)
- [get](./get.md)
- [merge](./merge.md)
- [pr](./pr.md)
- [switch](./switch.md)
