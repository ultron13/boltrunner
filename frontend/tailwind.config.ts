import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: 'class',
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        chrome: { DEFAULT: "var(--chrome-bg)", fg: "var(--chrome-fg)" },
        accent: "var(--accent)",
        surface: { DEFAULT: "var(--surface)", alt: "var(--surface-alt)" },
        border: "var(--border)",
        text: { DEFAULT: "var(--text)", muted: "var(--text-muted)" },
        status: {
          "pass-bg": "var(--status-pass-bg)",
          "pass-fg": "var(--status-pass-fg)",
          "warn-bg": "var(--status-warn-bg)",
          "warn-fg": "var(--status-warn-fg)",
          "fail-bg": "var(--status-fail-bg)",
          "fail-fg": "var(--status-fail-fg)",
          "info-bg": "var(--status-info-bg)",
          "info-fg": "var(--status-info-fg)",
        },
      },
    },
  },
  plugins: [],
};
export default config;
