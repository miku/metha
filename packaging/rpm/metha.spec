Summary:    No frills OAI mirror.
Name:       metha
Version:    0.2.21
Release:    0
License:    GPL
BuildArch:  x86_64
BuildRoot:  %{_tmppath}/%{name}-build
Group:      System/Base
Vendor:     Leipzig University Library, https://www.ub.uni-leipzig.de
URL:        https://github.com/miku/metha

%description

No frills incremental OAI harvesting for the command line.

%prep

%build

%pre

%install
mkdir -p $RPM_BUILD_ROOT/usr/local/sbin
install -m 755 metha-cat $RPM_BUILD_ROOT/usr/local/sbin
install -m 755 metha-id $RPM_BUILD_ROOT/usr/local/sbin
install -m 755 metha-sync $RPM_BUILD_ROOT/usr/local/sbin
install -m 755 metha-ls $RPM_BUILD_ROOT/usr/local/sbin
install -m 755 metha-files $RPM_BUILD_ROOT/usr/local/sbin
install -m 755 metha-fortune $RPM_BUILD_ROOT/usr/local/sbin

mkdir -p $RPM_BUILD_ROOT/usr/local/share/man/man1
install -m 644 metha.1 $RPM_BUILD_ROOT/usr/local/share/man/man1/metha.1
%post

%clean
rm -rf $RPM_BUILD_ROOT
rm -rf %{_tmppath}/%{name}
rm -rf %{_topdir}/BUILD/%{name}

%files
%defattr(-,root,root)

/usr/local/sbin/metha-cat
/usr/local/sbin/metha-id
/usr/local/sbin/metha-ls
/usr/local/sbin/metha-sync
/usr/local/sbin/metha-files
/usr/local/sbin/metha-fortune
/usr/local/share/man/man1/metha.1

%changelog
* Thu Apr 21 2016 Martin Czygan
- 0.1.0 initial release
