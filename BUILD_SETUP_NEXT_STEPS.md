# Build setup — one-time things to do before v0.1.0 lands publicly

These are the steps the scaffolding cannot do for you. Each is a five-to-ten-minute
chore, but all of them have to happen before the README points to live URLs.

## 1. Re-record the asciinema demo

The committed `docs/demo.cast` is a hand-edited placeholder so the README has
something to embed on day one. Replace it with a real recording:

```bash
brew install asciinema   # macOS; on Linux: apt install asciinema
./scripts/regenerate_demo.sh
```

That script builds a fresh binary into `./excise`, then runs
`scripts/demo.sh` under `asciinema rec`, overwriting `docs/demo.cast`.

Optional — generate a hi-fi GIF as a fallback for users who block third-party
asciinema embeds:

```bash
brew install charmbracelet/tap/vhs
vhs docs/demo.tape   # writes docs/demo.gif
```

Once the .cast is good, upload it to https://asciinema.org and replace the
placeholder URL in `README.md` (search for `ASCIINEMA-ID`).

## 2. Publish to `go install`

The Go proxy will pick up the module automatically as soon as a tag is pushed:

```bash
git tag v0.1.0
git push origin v0.1.0
```

After ~five minutes, `go install github.com/SuperMarioYL/excise/cmd/excise@latest`
will work for anyone.

## 3. Homebrew formula

Create a separate tap repository (one-time):

```bash
gh repo create SuperMarioYL/homebrew-tap --public
```

Drop the formula in `Formula/excise.rb` of that tap. A starter:

```ruby
class Excise < Formula
  desc "Surgical context editing for coding-agent transcripts"
  homepage "https://github.com/SuperMarioYL/excise"
  url "https://github.com/SuperMarioYL/excise/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "TODO_after_release"
  license "MIT"
  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/excise"
  end

  test do
    assert_match "0.1.0", shell_output("#{bin}/excise --version")
  end
end
```

Users then do `brew install supermarioyl/tap/excise`.

## 4. AUR PKGBUILD

```bash
mkdir excise-aur && cd excise-aur
cat > PKGBUILD <<'EOF'
pkgname=excise
pkgver=0.1.0
pkgrel=1
pkgdesc="Surgical context editing for coding-agent transcripts"
arch=('x86_64' 'aarch64')
url="https://github.com/SuperMarioYL/excise"
license=('MIT')
makedepends=('go')
source=("$url/archive/v$pkgver.tar.gz")
sha256sums=('SKIP')

build() {
  cd "$srcdir/$pkgname-$pkgver"
  go build -trimpath -ldflags="-s -w" -o excise ./cmd/excise
}

package() {
  install -Dm755 "$srcdir/$pkgname-$pkgver/excise" "$pkgdir/usr/bin/excise"
  install -Dm644 "$srcdir/$pkgname-$pkgver/LICENSE" "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
EOF
makepkg --printsrcinfo > .SRCINFO
```

Then push to AUR via SSH (one-time AUR account setup needed).

## 5. Show HN draft

The README's marketing copy (section 7 of the MVP plan) is the source of truth
for launch day. The post title is locked:

> Excise — surgical context editing for Claude Code sessions

Lead the body with the asciinema, then quote the Anthropic-docs claim about
">2 corrections poisons a session," then the GitHub link. Do not deviate.

## 6. Smoke test before pushing the tag

```bash
go build -o /tmp/excise ./cmd/excise
/tmp/excise list testdata/claude_session_with_tools.jsonl
/tmp/excise cut 5-7 --dry-run testdata/claude_session_with_tools.jsonl
/tmp/excise rollback --list
```

If any of those three commands errors, do NOT cut the release.
