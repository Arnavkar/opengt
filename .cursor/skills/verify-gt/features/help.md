# help

`gt help`, `gt --help`, `gt -h`, and `gt --version` identify the binary and list commands.

## Sub-features

- `help-text` lists every implemented command and says unknown commands go to git.
- `version` prints `gt <version> (opengt)`.

## How to get to it (user POV)

- Run `gt`, `gt help`, `gt --help`, or `gt -h`.
- Run `gt --version`.

## Driving it with gt-sandbox

Preconditions:

- `GT_BIN` from `gt-sandbox launch`. `REPO` may be empty for `--version`; help does not need a repo either, but `run` requires `REPO`. Use `new-repo` first.

- **Version.** Run `gt-sandbox run -- --version`. Exit `0`. stdout is `gt <version> (opengt)`.
- **Help.** Run `gt-sandbox run -- help`. Exit `0`. stdout contains `create`, `submit`, `sync`, `doctor`, and `Anything else is passed through to git`. stdout mentions linear stacks.
- **Proof.** Those transcripts. This is how you tell Graphite's `gt` apart from opengt.

## Gotchas

- Graphite's `gt --version` will not contain `(opengt)`. If doctor or help looks wrong, you are on the wrong binary.
- `gt <command> --help` is per-command and is an extra entry point for that command's feature file.
