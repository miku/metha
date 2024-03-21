Summary:    No frills OAI mirror.
Name:       metha
Version:    0.2.63
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
mkdir -p $RPM_BUILD_ROOT/usr/local/bin
install -m 755 metha-cat $RPM_BUILD_ROOT/usr/local/bin
install -m 755 metha-id $RPM_BUILD_ROOT/usr/local/bin
install -m 755 metha-sync $RPM_BUILD_ROOT/usr/local/bin
install -m 755 metha-ls $RPM_BUILD_ROOT/usr/local/bin
install -m 755 metha-files $RPM_BUILD_ROOT/usr/local/bin
install -m 755 metha-fortune $RPM_BUILD_ROOT/usr/local/bin

mkdir -p $RPM_BUILD_ROOT/usr/local/share/man/man1
install -m 644 metha.1 $RPM_BUILD_ROOT/usr/local/share/man/man1/metha.1

mkdir -p $RPM_BUILD_ROOT/usr/lib/systemd/system
install -m 644 metha.service $RPM_BUILD_ROOT/usr/lib/systemd/system/metha.service
%post

%clean
rm -rf $RPM_BUILD_ROOT
rm -rf %{_tmppath}/%{name}
rm -rf %{_topdir}/BUILD/%{name}

%files
%defattr(-,root,root)

/usr/local/bin/metha-cat
/usr/local/bin/metha-id
/usr/local/bin/metha-ls
/usr/local/bin/metha-sync
/usr/local/bin/metha-files
/usr/local/bin/metha-fortune
/usr/local/share/man/man1/metha.1
/usr/lib/systemd/system/metha.service

%changelog
* Thu Apr 21 2016 Martin Czygan
- 0.1.0 initial release
