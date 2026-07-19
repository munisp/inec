import { jsPDF } from "jspdf";
import autoTable from "jspdf-autotable";

export interface ExportColumn {
  header: string;
  key: string;
  width?: number;
}

/**
 * Export data to CSV and download it.
 */
export function exportToCSV(filename: string, columns: ExportColumn[], rows: Record<string, unknown>[]) {
  const headers = columns.map(c => `"${c.header}"`).join(",");
  const body = rows.map(row =>
    columns.map(c => {
      const val = row[c.key];
      if (val === null || val === undefined) return '""';
      const str = String(val).replace(/"/g, '""');
      return `"${str}"`;
    }).join(",")
  ).join("\n");
  const csv = `${headers}\n${body}`;
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `${filename}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * Export data to PDF using jsPDF + autoTable and download it.
 */
export function exportToPDF(
  filename: string,
  title: string,
  subtitle: string,
  columns: ExportColumn[],
  rows: Record<string, unknown>[]
) {
  const doc = new jsPDF({ orientation: "landscape", unit: "mm", format: "a4" });

  // Header bar
  doc.setFillColor(74, 21, 37); // INEC burgundy
  doc.rect(0, 0, 297, 20, "F");
  doc.setTextColor(255, 255, 255);
  doc.setFontSize(14);
  doc.setFont("helvetica", "bold");
  doc.text(title, 14, 13);

  // Subtitle + date
  doc.setFontSize(9);
  doc.setFont("helvetica", "normal");
  doc.text(subtitle, 14, 19);
  doc.text(`Generated: ${new Date().toLocaleString("en-NG")}`, 240, 19);

  // Table
  autoTable(doc, {
    startY: 26,
    head: [columns.map(c => c.header)],
    body: rows.map(row => columns.map(c => {
      const val = row[c.key];
      if (val === null || val === undefined) return "";
      return String(val);
    })),
    headStyles: {
      fillColor: [0, 135, 81], // INEC green
      textColor: 255,
      fontStyle: "bold",
      fontSize: 9,
    },
    bodyStyles: { fontSize: 8 },
    alternateRowStyles: { fillColor: [245, 240, 235] },
    columnStyles: Object.fromEntries(
      columns.map((c, i) => [i, { cellWidth: c.width ?? "auto" }])
    ),
    margin: { left: 14, right: 14 },
  });

  // Footer
  const pageCount = (doc as unknown as { internal: { getNumberOfPages: () => number } }).internal.getNumberOfPages();
  for (let i = 1; i <= pageCount; i++) {
    doc.setPage(i);
    doc.setFontSize(7);
    doc.setTextColor(150);
    doc.text(`INEC Campaign Intelligence Platform — Confidential`, 14, 205);
    doc.text(`Page ${i} of ${pageCount}`, 270, 205);
  }

  doc.save(`${filename}.pdf`);
}
