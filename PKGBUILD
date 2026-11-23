# Maintainer: adem <adem482p@users.noreply.github.com>
pkgname=justray
pkgver=r6.5dc9632
pkgrel=1
pkgdesc='A StatusNotifierItem host for Linux'
arch=('x86_64' 'aarch64')
url='https://github.com/adem482p/justray'
license=('unknown')
depends=('dbus' 'glibc' 'libgcc' 'libstdc++' 'qt6-base')
makedepends=('go>=1.27' 'git')
optdepends=(
  'hicolor-icon-theme: fallback icon theme'
  'qt6-svg: SVG icon theme support'
)
source=("git+https://github.com/adem482p/justray.git")
sha256sums=('SKIP')

pkgver() {
  cd "$pkgname"
  # Use commit count and short hash for version (no tags in repo)
  # Format: r<commit count>.<short hash>
  printf "r%s.%s" "$(git rev-list --count HEAD)" "$(git rev-parse --short=7 HEAD)"
}

build() {
  cd "$pkgname"
  export CGO_ENABLED=1
  export CGO_CPPFLAGS="${CPPFLAGS}"
  export CGO_CFLAGS="${CFLAGS}"
  export CGO_CXXFLAGS="${CXXFLAGS}"
  export CGO_LDFLAGS="${LDFLAGS}"
  export GOFLAGS="-buildmode=pie -trimpath -mod=readonly -modcacherw"
  export GOTOOLCHAIN=local
  go build -o tray ./cmd/tray
}

check() {
  cd "$pkgname"
  export CGO_ENABLED=1
  export GOTOOLCHAIN=local
  go test -tags debug ./...
}

package() {
  cd "$pkgname"
  install -Dm755 tray "$pkgdir/usr/bin/tray"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
}
