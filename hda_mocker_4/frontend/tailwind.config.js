/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: { extend: { colors: {
    background: 'hsl(var(--background))', foreground: 'hsl(var(--foreground))',
    card: 'hsl(var(--card))', primary: 'hsl(var(--primary))',
    muted: 'hsl(var(--muted))', mutedForeground: 'hsl(var(--muted-foreground))',
    border: 'hsl(var(--border))', destructive: 'hsl(var(--destructive))',
  } } },
  plugins: [],
}
