# Wattpilot Data Exporter ⚡

fetching data from wattpilot and calculating how much has been charged, based on the official numbers (1)

## Run it

Make sure you have the following environment variables set:
- ```WATTPILOT_KEY```

You can grab the key via your Wattpilot exporter website, it is the `?e=` parameter in the URL.
Eg. `https://data.wattpilot.io/export?e=THIS_IS_YOUR_KEY`

Run the application:
```bash
cd src
go run .
```

Alternatively, use the `makefile`:
```bash
cd src
make run
```

Access the application in your browser on http://localhost:8080





## Resources

1) https://www.bmf.gv.at/themen/steuern/arbeitnehmerinnenveranlagung/pendlerfoerderung-das-pendlerpauschale/sachbezug-kraftfahrzeug.html
