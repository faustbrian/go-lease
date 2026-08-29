.PHONY: docs fence-mutations backend-hardening

docs:
	./scripts/check-docs.sh

fence-mutations:
	./scripts/check-fence-mutations.sh

backend-hardening:
	./scripts/check-backend-faults.sh
