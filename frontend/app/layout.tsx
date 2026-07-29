import type { Metadata } from "next";
import localFont from "next/font/local";
import { Suspense } from "react";
import "./globals.css";
import { ThemeProvider } from "@/components/ui/theme";
import { ProjectProvider } from "@/components/ui/ProjectProvider";
import { Shell } from "@/components/ui/Shell";

const geistSans = localFont({
  src: "./fonts/GeistVF.woff",
  variable: "--font-geist-sans",
  weight: "100 900",
});
const geistMono = localFont({
  src: "./fonts/GeistMonoVF.woff",
  variable: "--font-geist-mono",
  weight: "100 900",
});

export const metadata: Metadata = {
  title: "BoltRunner",
  description: "Open-source, Kubernetes-native load testing.",
};

// Applies a persisted dark theme before hydration so there's no flash of the
// light theme on load for users who chose dark last time.
const THEME_INIT_SCRIPT = `
(function() {
  try {
    var stored = localStorage.getItem('boltrunner-theme');
    if (stored === 'dark') document.documentElement.classList.add('dark');
  } catch (e) {}
})();
`;

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body className={`${geistSans.variable} ${geistMono.variable} antialiased`}>
        <ThemeProvider>
          <ProjectProvider>
            <Suspense fallback={null}>
              <Shell>{children}</Shell>
            </Suspense>
          </ProjectProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
