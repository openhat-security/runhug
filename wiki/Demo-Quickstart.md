# Demo: Quickstart

Slow, readable first-run: version → help → wizard → packs → search → inspect → dry-run deploy.

![Quickstart demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/quickstart.gif)

**Tape source:** [`assets/quickstart.tape`](https://github.com/openhat-security/runhug/blob/main/assets/quickstart.tape) · **MP4:** [`quickstart.mp4`](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/quickstart.mp4)

## Re-record

```bash
make demo-vhs TAPE=quickstart
```

## Commands shown

```bash
runhug version
runhug
runhug wizard --yes
runhug packs list
runhug search -q 'small instruct' --limit 5
runhug inspect Qwen/Qwen2.5-0.5B-Instruct
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
```

← [All demos](Demos) · [Setup](Setup)
