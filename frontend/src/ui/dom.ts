// Tiny DOM helpers so the UI modules stay declarative without a framework.

type ElProps<K extends keyof HTMLElementTagNameMap> = Partial<
  Omit<HTMLElementTagNameMap[K], "style" | "className">
> & { class?: string; style?: string };

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: ElProps<K> = {},
  children: (Node | string)[] = [],
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  const { class: className, style, ...rest } = props;
  if (className) node.className = className;
  if (style) node.style.cssText = style;
  Object.assign(node, rest);
  for (const child of children) {
    node.append(typeof child === "string" ? document.createTextNode(child) : child);
  }
  return node;
}

/** Mount an overlay card; returns a close() that removes it. */
export function overlay(card: HTMLElement, dismissible = true): () => void {
  const back = el("div", { class: "overlay" }, [card]);
  document.body.append(back);
  const close = () => back.remove();
  if (dismissible) {
    back.addEventListener("click", (e) => {
      if (e.target === back) close();
    });
  }
  return close;
}
