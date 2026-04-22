import json

with open('config.json', 'r') as f:
    cfg = json.load(f)

secrets = {
    'OIDC_CLIENT_SECRET': cfg.get('OIDC_CLIENT_SECRET', ''),
    'ZITADEL_SERVICE_PAT': cfg.get('ZITADEL_SERVICE_PAT', ''),
}

with open('.env', 'a') as f:
    for k, v in secrets.items():
        if v:
            f.write(f'{k}="{v}"\n')

# Only remove from json if present
if 'OIDC_CLIENT_SECRET' in cfg:
    del cfg['OIDC_CLIENT_SECRET']
if 'ZITADEL_SERVICE_PAT' in cfg:
    del cfg['ZITADEL_SERVICE_PAT']

with open('config.json', 'w') as f:
    json.dump(cfg, f, indent=4)
