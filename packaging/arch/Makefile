VERSION = 0.1.31
RELEASE = 1

metha-$(VERSION)-$(RELEASE)-x86_64.pkg.tar.xz: PKGBUILD
	makepkg

clean:
	rm -f metha-$(VERSION)-$(RELEASE)-x86_64.pkg.tar.xz
	rm -f v$(VERSION).tar.gz
	rm -rf pkg/
	rm -rf src/
