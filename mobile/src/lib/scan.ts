import { Camera, CameraResultType, CameraSource } from "@capacitor/camera"
import jsQR from "jsqr"

import { parseOffer } from "./offer"

export async function decodeQRFromDataUrl(dataUrl: string): Promise<string> {
  const img = new Image()
  await new Promise<void>((resolve, reject) => {
    img.onload = () => resolve()
    img.onerror = () => reject(new Error("could not load image"))
    img.src = dataUrl
  })
  const canvas = document.createElement("canvas")
  canvas.width = img.naturalWidth || img.width
  canvas.height = img.naturalHeight || img.height
  const ctx = canvas.getContext("2d")
  if (!ctx) throw new Error("no canvas")
  ctx.drawImage(img, 0, 0)
  const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height)
  const code = jsQR(imageData.data, imageData.width, imageData.height)
  if (!code?.data) throw new Error("no pairlink offer in image")
  return code.data
}

export async function scanPairlinkURI(): Promise<string> {
  const photo = await Camera.getPhoto({
    quality: 90,
    allowEditing: false,
    resultType: CameraResultType.DataUrl,
    source: CameraSource.Camera,
  })
  if (!photo.dataUrl) throw new Error("camera returned no image")
  const uri = await decodeQRFromDataUrl(photo.dataUrl)
  parseOffer(uri)
  return uri
}
