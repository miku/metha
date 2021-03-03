NAME = metha
VERSION = 0.2.21
RELEASE = 0

$(NAME)-$(VERSION)-$(RELEASE)-x86_64.pkg.tar.xz: PKGBUILD
	makepkg

# Note: src dir removal might require superuser (cf. go clean -modcache).
clean:
	rm -f $(NAME)-$(VERSION)-$(RELEASE)-x86_64.pkg.tar.xz
	rm -f v$(VERSION).tar.gz
	rm -rf pkg/
	rm -rf src/
