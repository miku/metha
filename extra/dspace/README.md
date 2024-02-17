# DSpace

* [via inurl](https://www.google.com/search?q=inurl%3A%22dspace-oai%2Frequest%22)

DSpace uses contexts, e.g. here for NUSL, summon, etc: http://digilib.k.utb.cz/oai

From https://dspace.lyrasis.org/wp-content/uploads/2022/11/AR-2022-DSpace.pdf

Total know installations: 3,199.

Our list contains about 669 sites.

> https://registry.lyrasis.org/

No download options, seemingly.

Last page number:

```sh
$ curl -sL "https://registry.lyrasis.org/?pagenum=1&mode=all" | pup 'a.page-numbers json{}' | jq -rc '.[2].text'
150
```

## Misc

The "/digital/bitstream"

* [](https://www.google.com/search?q=inurl%3A%22%2Fdigital%2Fbitstream%2F%22&sca_esv=8c3ea7e49633cccf&source=hp&ei=leHQZajDOqqVxc8P64K3mAQ&iflsig=ANes7DEAAAAAZdDvpo5EKU1w4w728K5T56d-QLzbLKpT&ved=0ahUKEwjo4amx6LKEAxWqSvEDHWvBDUMQ4dUDCBc&uact=5&oq=inurl%3A%22%2Fdigital%2Fbitstream%2F%22&gs_lp=Egdnd3Mtd2l6IhtpbnVybDoiL2RpZ2l0YWwvYml0c3RyZWFtLyJIyKUBUABY0aABcAN4AJABAJgBhwKgAasVqgEGMjYuMy4xuAEDyAEA-AEB-AECwgIFEAAYgATCAgsQLhiABBjHARjRA8ICBRAuGIAEwgIIEC4YgAQY1ALCAgcQABiABBgKwgIOEC4YrwEYxwEYgAQYjgU&sclient=gws-wiz)
