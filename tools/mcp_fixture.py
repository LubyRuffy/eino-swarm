#!/usr/bin/env python3
"""Private, deterministic stdio MCP fixture for UI and native acceptance.

No filesystem, subprocess or network tools are exposed. Stdout belongs solely
to the line-delimited JSON-RPC protocol.
"""
import json
import sys


def response(request):
    method = request.get("method")
    if "id" not in request:
        return None
    if method == "initialize":
        result = {"protocolVersion": request["params"]["protocolVersion"],
                  "capabilities": {"tools": {}},
                  "serverInfo": {"name": "zwai-acceptance-fixture", "version": "1"}}
    elif method == "tools/list":
        result = {"tools": [{"name": "echo", "description": "Return the supplied acceptance text.",
                              "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}},
                                              "required": ["text"]}}]}
    elif method == "tools/call" and request.get("params", {}).get("name") == "echo":
        result = {"content": [{"type": "text", "text": request["params"]["arguments"]["text"]}]}
    elif method == "ping":
        result = {}
    else:
        return {"jsonrpc": "2.0", "id": request["id"],
                "error": {"code": -32601, "message": "Unknown fixture method"}}
    return {"jsonrpc": "2.0", "id": request["id"], "result": result}


def main():
    for line in sys.stdin:
        reply = response(json.loads(line))
        if reply is not None:
            print(json.dumps(reply), flush=True)


if __name__ == "__main__":
    main()
