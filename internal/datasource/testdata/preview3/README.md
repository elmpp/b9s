Real output of Graph-mode Preview 3 bd (donnabox/beads 210f8dad, "bd version
1.3.0") for a fresh `bd init --graph-mode link` workspace holding three Memory
beads joined by a `defines` and a `references` Link, and two Issues joined by a
`blocks` dependency. The tests parse these files, so a parser change is
checked against what bd prints rather than against a guess.

metadata.json had its `graph_workspace` path replaced with a neutral one.
