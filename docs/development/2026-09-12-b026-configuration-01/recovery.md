# Import failure recovery

The importer never removes files after a partial failure. Its error output lists every path that this invocation created and still knows about.

Do not print, open, copy, hash, or otherwise inspect file contents. Check only each listed path's metadata with `lstat` or `stat -c '%a %U %F %s %n'` without following symbolic links. Stop if a path is absent, is a symbolic link, is not a regular file, is not owned by the current user, or has a mode other than `0600`.

Create a new unique recovery directory inside `~/.config/repomesh` with mode `0700`. Move every listed created path into that directory using the exact paths printed by the failed invocation. Do not overwrite any name in the recovery directory. After all fixed destination paths are absent, rerun the importer in a terminal.

Keep the recovery directory private for manual investigation or later deletion. Do not display secret or PEM contents. This recovery procedure does not read or change `root-1.key`, `runtime.env`, the final `auth.json`, PostgreSQL, or any running RepoMesh process.
