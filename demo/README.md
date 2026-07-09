# demo/

A self-contained, reproducible tour of `back-then`.

## Files

- **`demo.sh`** — builds the binary, creates a throwaway file tree with
  back-dated timestamps (three "bursts" from different moments), and walks
  through `index → stats → sessions → find → near`. It works entirely inside a
  temp directory and **never touches your real files or index**. Run it with:

  ```sh
  ./demo/demo.sh
  ```

  Tunables via env vars: `DEMO_PAUSE` (seconds between steps, default `1.1`).

- **`back-then.cast`** — an [asciicast v2](https://docs.asciinema.org/manual/asciicast/v2/)
  recording of `demo.sh`. Play it locally:

  ```sh
  asciinema play demo/back-then.cast
  ```

  or open it with the [asciinema player](https://docs.asciinema.org/manual/player/).

## Regenerating the cast

```sh
asciinema rec --overwrite \
  --cols 96 --rows 32 --idle-time-limit 1.5 \
  -t "back-then — a local-first time machine for your files" \
  -c "./demo/demo.sh" \
  demo/back-then.cast
```

Because the file tree is generated deterministically, re-recording produces the
same commands and output (only the absolute temp paths and the exact clock
times differ from run to run).
