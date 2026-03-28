import requests
import json
import time

SERVER_URL = "http://192.168.1.240:8004/mcp"

def test_mcp_streamable():
    print(f"🚀 Testing Mnemosyne MCP (Streamable HTTP) at {SERVER_URL}")

    # 1. Test Tools List
    print("\n--- Testing 'tools/list' ---")
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/list",
        "params": {}
    }
    try:
        # StreamableHTTP uses specific endpoints per method
        response = requests.post(f"{SERVER_URL}/tools/list", json=payload, timeout=10)
        print(f"Status: {response.status_code}")
        if response.status_code == 200:
            print(f"✅ Tools: {json.dumps(response.json(), indent=2)}")
        else:
            print(f"❌ Failed: {response.text}")
    except Exception as e:
        print(f"❌ Connection Failed: {e}")
        return

    # 2. Test Ingest Memory (Asynchronous)
    print("\n--- Testing 'ingest_memory' (Async) ---")
    ingest_payload = {
        "jsonrpc": "2.0",
        "id": 2,
        "method": "tools/call",
        "params": {
            "name": "ingest_memory",
            "arguments": {
                "content": "### TAZLAB TEST: STREAMABLE HTTP\nVerification of the new Streamable HTTP transport and asynchronous ingestion.",
                "timestamp": "2026-03-28T23:30:00Z"
            }
        }
    }
    response = requests.post(f"{SERVER_URL}/tools/call", json=ingest_payload, timeout=10)
    print(f"Status: {response.status_code}")
    if response.status_code == 200:
        print(f"✅ Response: {json.dumps(response.json(), indent=2)}")
    else:
        print(f"❌ Failed: {response.text}")

    # 3. Test List Memories (to verify)
    time.sleep(2) # Wait for worker
    print("\n--- Testing 'list_memories' ---")
    list_payload = {
        "jsonrpc": "2.0",
        "id": 3,
        "method": "tools/call",
        "params": {
            "name": "list_memories",
            "arguments": {"limit": 3}
        }
    }
    response = requests.post(f"{SERVER_URL}/tools/call", json=list_payload, timeout=10)
    if response.status_code == 200:
        print(f"✅ List Result: {json.dumps(response.json(), indent=2)}")

    print("\n✨ Test sequence finished.")

if __name__ == "__main__":
    test_mcp_streamable()
