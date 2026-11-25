# README screenshots

Rendered headlessly from the real TUI models, so they're reproducible and
never show private repo names.

```bash
scripts/screenshots/demo-workspace.sh /tmp/lts-demo          # fictional repos, every status
LTS_DEMO=/tmp/lts-demo SHOTS=/tmp/lts-shots \
  go test -tags screenshots -run TestScreenshots ./internal/app
# optional CLI frame:
{ printf '\033[38;2;120;200;140m~/projects\033[0m ❯ lts status --all\n';
  LTS_CONFIG_DIR=$(mktemp -d) lts status --all --dir /tmp/lts-demo; } > /tmp/lts-shots/cli.ans
python3 scripts/screenshots/render.py /tmp/lts-shots docs/images --title "lts — ~/projects" --cols 132
```

`render.py` needs only Pillow; frames are 132×40 (Explorer 132×30).
