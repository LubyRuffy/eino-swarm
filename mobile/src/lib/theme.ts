export function applySystemTheme(): () => void {
  const mq = window.matchMedia("(prefers-color-scheme: dark)")
  const paint = () => {
    document.documentElement.classList.toggle("dark", mq.matches)
  }
  paint()
  mq.addEventListener("change", paint)
  return () => mq.removeEventListener("change", paint)
}
