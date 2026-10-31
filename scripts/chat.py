#!/usr/bin/env python3
"""Interactive terminal client for the guaperrimo backend.

Uploads a photo, runs the photo turn, then lets you answer the stylist by
typing (voice/text) or by picking a button number. Only the standard library.

  python3 scripts/chat.py --photo foto.jpg
  python3 scripts/chat.py --photo foto.jpg --location 19.4194,-99.1616 --label "Roma Norte"
  python3 scripts/chat.py --photo foto.jpg --base http://192.168.100.39:8080 --api-key secreto
"""
import argparse
import json
import mimetypes
import sys
import urllib.error
import urllib.request
import uuid


def request(method, url, body=None, content_type=None, api_key=None, timeout=240):
    req = urllib.request.Request(url, data=body, method=method)
    if content_type:
        req.add_header("Content-Type", content_type)
    if api_key:
        req.add_header("X-API-Key", api_key)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except json.JSONDecodeError:
            return e.code, {"error": "non-json response"}


def upload(base, session, photo, api_key):
    boundary = "----guaperrimo" + uuid.uuid4().hex
    ctype = mimetypes.guess_type(photo)[0] or "image/jpeg"
    with open(photo, "rb") as f:
        data = f.read()
    body = (
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="image"; filename="{photo.split("/")[-1]}"\r\n'
        f"Content-Type: {ctype}\r\n\r\n"
    ).encode() + data + f"\r\n--{boundary}--\r\n".encode()
    return request("POST", f"{base}/session/{session}/image", body, f"multipart/form-data; boundary={boundary}", api_key)


def chat(base, session, payload, api_key):
    return request("POST", f"{base}/session/{session}/chat", json.dumps(payload).encode(), "application/json", api_key)


def show_turn(resp):
    print("\n" + "=" * 70)
    print(f"[turno {resp.get('turn')} · {resp.get('phase')} · input_mode={resp.get('input_mode')}]")
    print("\nESTILISTA:", resp.get("message", ""))
    opts = resp.get("options") or []
    for i, o in enumerate(opts, 1):
        print(f"   {i}. {o['label']}  ({o['id']})")
    if resp.get("is_final"):
        show_final(resp)
    if resp.get("input_mode") == "preview":
        show_direction(resp.get("direction") or {})
    return opts


def show_direction(d):
    brief = d.get("brief") or {}
    print("\n--- TU DIRECCIÓN DE ESTILO ---")
    print(brief.get("summary", ""))
    if brief.get("color_season"):
        print("estación:", brief["color_season"])
    print("paleta:", ", ".join(f"{c['name']} {c.get('hex', '')}" for c in brief.get("palette") or []))
    if brief.get("avoid"):
        print("evitar:", ", ".join(f"{c['name']} ({c.get('why', '')})" for c in brief["avoid"]))
    if brief.get("silhouette"):
        print("silueta:", brief["silhouette"])
    print("\n--- IDEAS (elige las que te llamen la atención) ---")
    for n, i in enumerate(d.get("ideas") or [], 1):
        colors = ", ".join(f"{c['name']} {c.get('hex', '')}" for c in i.get("colors") or [])
        print(f"\n{n}. [{i['slot']}] {i['garment']} · {colors} · {', '.join(i.get('textures') or [])} · {i.get('fit', '')}")
        print(f"   por qué: {i.get('why', '')}")
        if i.get("price_max_mxn"):
            print(f"   rango: ${i.get('price_min_mxn', 0):.0f}-{i['price_max_mxn']:.0f} MXN")
        ref = i.get("reference")
        if ref:
            print(f"   referencia: {ref['title']} · ${ref['price']:.0f} {ref['currency']} · {ref['store']}")
            print(f"   foto: {ref.get('thumbnail', '')}")
        else:
            print("   (sin foto de referencia)")


def show_final(resp):
    print("\n--- RECOMENDACIÓN FINAL ---")
    for a in resp.get("priority_actions") or []:
        print(f"• [{a['impact']}/{a['effort']}] {a['title']}: {a['description']}")
    print("\n--- LISTA DE COMPRAS ---")
    for item in resp.get("shopping_list") or []:
        print(f"\n{item.get('priority', '-')}. {item['slot']}: {item['description']}")
        if item.get("why"):
            print(f"   por qué: {item['why']}")
        for p in item.get("products") or []:
            near = p.get("nearby_store")
            where = f"{near['name']} a {near['distance_m']} m" if near else (p.get("delivery") or "en línea")
            print(f"   - ${p['price']:.0f} {p['currency']} · {p['title']} · {p['store']} · {where}")
            print(f"     {p.get('link', '')}")
    if resp.get("total_mxn"):
        print(f"\nTOTAL (opción principal de cada artículo): ${resp['total_mxn']:.0f} MXN")
    stores = resp.get("stores") or []
    if stores:
        print("\n--- TIENDAS CERCANAS CON ALGO DE LA LISTA ---")
        for s in stores:
            print(f"• {s['name']} · {s['distance_m']} m · {s.get('address', '')} · {s.get('open_state', '')}")
    if resp.get("looks_generating"):
        print("\n(looks con try-on generándose en segundo plano: GET /session/{id}/looks)")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    ap.add_argument("--photo", required=True, help="full-body photo (jpg/png/webp)")
    ap.add_argument("--session", default=None)
    ap.add_argument("--location", default=None, help="lat,lng of the user (sent on the first turn)")
    ap.add_argument("--label", default=None, help="human label for the location")
    ap.add_argument("--api-key", default=None)
    args = ap.parse_args()

    session = args.session or str(uuid.uuid4())
    print(f"sesión: {session}")

    code, resp = upload(args.base, session, args.photo, args.api_key)
    if code != 200:
        sys.exit(f"upload falló ({code}): {resp}")
    print("foto subida:", resp.get("url"))

    first = {"type": "image"}
    if args.location:
        lat, lng = (float(x) for x in args.location.split(","))
        first["location"] = {"lat": lat, "lng": lng, "label": args.label or ""}
    print("analizando la foto y arrancando al estilista...")
    code, resp = chat(args.base, session, first, args.api_key)
    if code != 200:
        sys.exit(f"turno de foto falló ({code}): {resp}")
    opts = show_turn(resp)
    ideas = (resp.get("direction") or {}).get("ideas") or []
    preview = resp.get("input_mode") == "preview"

    while True:
        try:
            prompt = "números de ideas separados por coma, y opcional '| comentario'" if preview else "número de botón o texto"
            answer = input(f"\nTÚ ({prompt}, vacío para salir): ").strip()
        except EOFError:
            answer = ""
        if not answer:
            print("\nfin. Recupera la recomendación con:")
            print(f"  curl -s {args.base}/session/{session}/recommendation")
            return
        if preview:
            picks, _, comment = answer.partition("|")
            nums = [int(x) for x in picks.replace(" ", "").split(",") if x.isdigit()]
            payload = {"type": "idea_selection", "selected_ideas": [ideas[n - 1]["id"] for n in nums if 1 <= n <= len(ideas)]}
            if comment.strip():
                payload["text"] = comment.strip()
        elif answer.isdigit() and opts and 1 <= int(answer) <= len(opts):
            payload = {"type": "button_response", "option_id": opts[int(answer) - 1]["id"]}
        else:
            payload = {"type": "voice_response", "transcript": answer}
        print("pensando...")
        code, resp = chat(args.base, session, payload, args.api_key)
        if code != 200:
            print(f"error {code}: {resp}")
            continue
        opts = show_turn(resp)
        ideas = (resp.get("direction") or {}).get("ideas") or []
        preview = resp.get("input_mode") == "preview"


if __name__ == "__main__":
    main()
