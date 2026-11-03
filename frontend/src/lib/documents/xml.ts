export function parseXml(text: string): XMLDocument {
  const doc = new DOMParser().parseFromString(text, "application/xml")
  if (doc.getElementsByTagName("parsererror").length > 0) throw new Error("document xml is invalid")
  return doc
}

export function elementsByLocal(root: ParentNode, local: string): Element[] {
  const found: Element[] = []
  const walk = (node: ParentNode) => {
    for (const child of node.children) {
      if (child.localName === local) found.push(child)
      walk(child)
    }
  }
  walk(root)
  return found
}

export function attrLocal(element: Element, local: string): string {
  for (const attr of element.attributes) {
    if (attr.localName === local) return attr.value
  }
  return ""
}

export function directChildren(element: Element, local: string): Element[] {
  return [...element.children].filter((child) => child.localName === local)
}
