# AUR (`runhug-bin`)

Binary package for Arch Linux / pacman. Publish to the AUR once (requires an AUR account):

```bash
# After bumping pkgver/sha256sums in PKGBUILD:
git clone ssh://aur@aur.archlinux.org/runhug-bin.git
cp PKGBUILD .SRCINFO runhug-bin/
cd runhug-bin
makepkg --printsrcinfo > .SRCINFO
git add PKGBUILD .SRCINFO
git commit -m "runhug-bin $pkgver"
git push
```

Users:

```bash
yay -S runhug-bin
# or
paru -S runhug-bin
```
