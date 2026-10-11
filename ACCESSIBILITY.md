# Accessibility

sslsync is a command-line tool. We want everyone to be able to run it and understand its output, including people using screen readers, high-contrast or monochrome terminals, and people reading English as a second language.

## What we do

- **Information is never only in color or symbols.** Every ✓ / ✗ / ↺ / ⚠ line also carries a word (`updated`, `check-failed`, `rolled-back`, `OLD`, `NEW`), so screen readers and monochrome terminals lose nothing.
- **Plain, linear text output.** No spinners, progress bars, cursor movement or full-screen interfaces. Every line can be read in order, copied, and searched.
- **Machine-readable output** in `logs/<run>/summary.json`, for anyone who prefers their own tools or formats.
- **Errors say what to do**, naming the exact variable or command, in plain language without jargon where possible.
- **Exit codes** (`0` success, `1` failure) for scripting and automation.
- **Help everywhere**: `make` lists every command, `make targets` every setting, and `.env.example` documents every variable next to an example.
- **Documentation** uses headings, tables with header rows, and descriptive link text, so it works with screen readers and in plain-text viewers.

## Known limitations

- The `make help` headings use ANSI bold/colour. If your terminal or screen reader shows raw codes, use `make help | sed 's/\x1b\[[0-9;]*m//g'`. The tool's own output (`bin/sslsync`) uses no colour codes.
- Output is in English. Documentation questions in Bahasa Indonesia are welcome in issues.

## Feedback

If anything in the tool or its documentation is hard to use with your setup, open an issue with the **Accessibility** label, or contact the maintainers directly. Accessibility issues are treated as bugs.
