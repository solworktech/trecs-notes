# Vendored syntax definitions

These are the YAML lexers from [jessp01/gohighlight](https://github.com/jessp01/gohighlight)
(`syntax_files/`, AGPL-3.0, derived from the [micro](https://micro-editor.github.io)
editor's definitions), embedded into the binary so the app needs no files at
run time. To update them, copy `syntax_files/*.yaml` from that repository over
this directory.

The files are kept yamllint-clean (see `.yamllint`; only line length is
exempt, as a lexer rule is a single regular expression). Reformatting does
not change what a file means. After updating from gohighlight, run
`yamllint internal/render/syntax_files`.
