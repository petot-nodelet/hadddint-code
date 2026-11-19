/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/**/*.{go,templ}", "./cmd/**/*.go"],
  theme: {
    extend: {
      colors: {
        canvas: "#09090b",
        ink: "#f4f4f5",
        "ink-muted": "#a1a1aa",
        gold: "#d4af69",
        sand: "#151517",
        line: "#29292d",
      },
      fontFamily: {
        sans: ["Plus Jakarta Sans", "sans-serif"],
        serif: ["Cormorant Garamond", "serif"],
      },
    },
  },
  plugins: [],
};
