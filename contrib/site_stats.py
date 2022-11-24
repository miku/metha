import pandas as pd

pd.set_option('display.max_rows', None)

df = pd.read_json("sites.json", lines=True)
print(df.groupby("tld").size().sort_values(ascending=False).head(25).to_markdown())

print()

tep = df.groupby(["tld", "is_edu", "platform"], as_index=False).size().sort_values(by="size", ascending=False).reset_index(drop=True)
print(tep.to_markdown())
