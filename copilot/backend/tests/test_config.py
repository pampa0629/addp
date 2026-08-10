from config import Settings


def test_gateway_url_uses_explicit_value_or_service_host():
    assert Settings(gateway_url="http://gateway:8000").get_gateway_url() == "http://gateway:8000"
    assert Settings(service_host="localhost", gateway_port=9000).get_gateway_url() == "http://localhost:9000"
