import type { Config } from "tailwindcss"
import animate from "tailwindcss-animate"

// Every colour is a CSS variable defined in src/index.css. Nothing in the app
// may name a colour directly: the light and dark themes have to stay in sync,
// and a hardcoded hex is the one thing that cannot.
export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        sidebar: {
          DEFAULT: "hsl(var(--sidebar))",
          foreground: "hsl(var(--sidebar-foreground))",
          accent: "hsl(var(--sidebar-accent))",
          border: "hsl(var(--sidebar-border))",
        },
        // Agent status has to read at a glance in both themes, so the three
        // states are tokens rather than ad-hoc greens and ambers.
        running: "hsl(var(--running))",
        done: "hsl(var(--done))",
        failed: "hsl(var(--failed))",
        file: {
          folder: "hsl(var(--file-folder))",
          code: "hsl(var(--file-code))",
          doc: "hsl(var(--file-doc))",
          config: "hsl(var(--file-config))",
          image: "hsl(var(--file-image))",
        },
        syntax: {
          keyword: "hsl(var(--syntax-keyword))",
          command: "hsl(var(--syntax-command))",
          string: "hsl(var(--syntax-string))",
          comment: "hsl(var(--syntax-comment))",
          operator: "hsl(var(--syntax-operator))",
          flag: "hsl(var(--syntax-flag))",
          variable: "hsl(var(--syntax-variable))",
          prompt: "hsl(var(--syntax-prompt))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      fontFamily: {
        sans: ["var(--font-sans)"],
        mono: ["var(--font-mono)"],
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        // The "working" dot: a slow pulse reads as activity without pulling
        // the eye away from the text that is streaming in.
        breathe: {
          "0%, 100%": { opacity: "1", transform: "scale(1)" },
          "50%": { opacity: "0.45", transform: "scale(0.82)" },
        },
        // Live status that does not fit: one copy slides out, the duplicate
        // takes its place, so the loop has no jump.
        marquee: {
          from: { transform: "translateX(0)" },
          to: { transform: "translateX(-50%)" },
        },
        // Light sweeping across a live status line. The row is otherwise
        // still, and a still row looks stuck.
        shimmer: {
          from: { backgroundPosition: "200% center" },
          to: { backgroundPosition: "-200% center" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
        breathe: "breathe 1.6s ease-in-out infinite",
        marquee: "marquee 12s linear infinite",
        shimmer: "shimmer 2.2s ease-in-out infinite",
      },
    },
  },
  plugins: [animate],
} satisfies Config
