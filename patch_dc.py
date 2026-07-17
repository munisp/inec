import yaml
with open('docker-compose.yml') as f:
    dc = yaml.safe_load(f)

# Remove APISIX public ports
if 'ports' in dc['services']['apisix']:
    del dc['services']['apisix']['ports']

# Add Caddy service
dc['services']['caddy'] = {
    'build': {
        'context': './caddy',
        'dockerfile': 'Dockerfile'
    },
    'ports': [
        '80:80',
        '443:443',
        '443:443/udp'
    ],
    'volumes': [
        './config/caddy/Caddyfile:/etc/caddy/Caddyfile:ro',
        './config/caddy/coraza.conf:/etc/caddy/coraza.conf:ro',
        'caddy-data:/data',
        'caddy-config:/config'
    ],
    'networks': ['inec-network'],
    'depends_on': ['redis', 'apisix', 'keycloak'],
    'restart': 'unless-stopped',
    'environment': [
        'CADDY_ADMIN=0.0.0.0:2019'
    ]
}

# Add caddy volumes
if 'volumes' not in dc:
    dc['volumes'] = {}
dc['volumes']['caddy-data'] = None
dc['volumes']['caddy-config'] = None

with open('docker-compose.yml', 'w') as f:
    yaml.dump(dc, f, default_flow_style=False, sort_keys=False)
