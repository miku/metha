
def cleaned(filename):
    with open("OJS beacon journals - Sheet1.tsv") as f:
        for line in f:
            line = line.strip()
            parts = line.split()
            for url in parts:
                if not url.startswith("http"):
                    url = "http://" + url
                yield url

if __name__ == '__main__':
    for line in cleaned("OJS beacon journals - Sheet1.tsv"):
        print(line)
