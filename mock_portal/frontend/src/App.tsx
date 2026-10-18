import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom"

import { Shell } from "@/components/shell"
import { ToastProvider } from "@/components/toast"
import { TooltipProvider } from "@/components/ui/tooltip"
import { MocksProvider } from "@/data/mocks"
import { MockDetailPage } from "@/pages/mock-detail"
import { MockListPage } from "@/pages/mock-list"
import { StandardizationPage } from "@/pages/standardization"

export default function App() {
  return (
    <ToastProvider>
      <TooltipProvider delayDuration={150}>
        <BrowserRouter>
          <MocksProvider>
            <Shell>
              <Routes>
                <Route path="/" element={<MockListPage />} />
                <Route path="/mocks/:id" element={<MockDetailPage />} />
                <Route path="/standardization" element={<StandardizationPage />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Shell>
          </MocksProvider>
        </BrowserRouter>
      </TooltipProvider>
    </ToastProvider>
  )
}
