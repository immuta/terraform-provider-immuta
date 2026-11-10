# Connections are imported with their connection key. Immuta never returns
# secrets, so the first apply after an import sends the configured
# credential, which makes Immuta crawl the connection again.
terraform import immuta_connection.trino trino
