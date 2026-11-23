# Maintainer: adem <adem482p@users.noreply.github.com>
pkgname=tray
pkgver=r2.g3a77c66
pkgrel=1
pkgdesc='A StatusNotifierItem host for Linux'
arch=('x86_64' 'aarch64')
url='https://github.com/adem482p/tray'
license=('unknown')
depends=('dbus' 'glibc' 'libgcc' 'libstdc++' 'qt6-base')
makedepends=('go>=1.27')
optdepends=(
  'hicolor-icon-theme: fallback icon theme'
  'qt6-svg: SVG icon theme support'
)
_commit=3a77c661c08b4164b4cd7290f400517af6f8aa42
source=("$pkgname-$pkgver.tar.gz::https://github.com/adem482p/tray/archive/$_commit.tar.gz")
sha256sums=('b4878f8bd8d1fe8979aabedfb3b0c00bf76ef2251b16280b5faef8616612405e')

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
