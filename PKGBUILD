# Maintainer: adem <adem482p@users.noreply.github.com>
pkgname=justray
pkgver=r2.g3a77c66
pkgrel=1
pkgdesc='A StatusNotifierItem host for Linux'
arch=('x86_64' 'aarch64')
url='https://github.com/adem482p/justray'
license=('unknown')
depends=('dbus' 'glibc' 'libgcc' 'libstdc++' 'qt6-base')
makedepends=('go>=1.27')
optdepends=(
  'hicolor-icon-theme: fallback icon theme'
  'qt6-svg: SVG icon theme support'
)
_commit=3a77c661c08b4164b4cd7290f400517af6f8aa42
source=("$pkgname-$pkgver.tar.gz::https://github.com/adem482p/justray/archive/$_commit.tar.gz")
sha256sums=('b0f4d62282d34975de67f73d3d184f2c119b3bdf6b4f7eef690f2c784ee4ffe4')

build() {
  cd "$pkgname-${_commit}"
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
  cd "$pkgname-${_commit}"
  export CGO_ENABLED=1
  export GOTOOLCHAIN=local
  go test -tags debug ./...
}

package() {
  cd "$pkgname-${_commit}"
  install -Dm755 tray "$pkgdir/usr/bin/tray"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
}
