from config import Settings


def test_gateway_url_uses_explicit_value_or_service_host():
    assert Settings(gateway_url="http://gateway:8000").get_gateway_url() == "http://gateway:8000"
    assert Settings(service_host="localhost", gateway_port=9000).get_gateway_url() == "http://localhost:9000"


def test_database_connection_uses_password_auth_without_implicit_host_kerberos(monkeypatch):
    # The real SQLAlchemy dialect exposes the arguments handed to libpq without
    # connecting. Ambient Kerberos credentials must not choose another protocol.
    from sqlalchemy import event
    from database import open_runtime
    for name, value in {"ADDP_DATABASE_RUNTIME_USER": "test", "ADDP_DATABASE_RUNTIME_PASSWORD": "test",
                        "POSTGRES_HOST": "localhost", "POSTGRES_PORT": "5432", "POSTGRES_DB": "test"}.items():
        monkeypatch.setenv(name, value)
    engine = open_runtime()

    observed = {}
    def inspect_connect(_dialect, _record, _args, kwargs):
        observed.update(kwargs)
        raise RuntimeError("connection inspected before DBAPI")

    event.listen(engine, "do_connect", inspect_connect)
    try:
        import pytest
        with pytest.raises(RuntimeError, match="connection inspected"):
            engine.connect()
    finally:
        event.remove(engine, "do_connect", inspect_connect)
    assert observed["gssencmode"] == "disable"
    assert observed["connect_timeout"] == 5
    assert "password" in observed
    assert "sslmode" not in observed


def test_development_entry_does_not_inject_an_unused_database_url():
    from pathlib import Path
    source = (Path(__file__).resolve().parents[3] / "scripts/dev/start.sh").read_text()
    assert "export DATABASE_URL=" not in source
