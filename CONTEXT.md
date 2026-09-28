# Domain glossary

## Finished stack

A stack whose every branch has a pull request, and every one of those pull requests is merged or closed.

Sync deletes a finished stack as a whole: the branches and the stack entry go together. A stack with one open pull request, or a branch with no pull request, is not finished. Merged branches on that stack stay, so the chain remains intact for restack.
