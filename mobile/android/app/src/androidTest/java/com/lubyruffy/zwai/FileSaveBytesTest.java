package com.lubyruffy.zwai;

import static org.junit.Assert.*;
import android.util.Base64;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Runtime byte contract only; this does not accept the system document-picker UI. */
@RunWith(AndroidJUnit4.class)
public class FileSaveBytesTest {
    @Test
    public void nativeDecoderKeepsEmptyBinaryAndUnicodeBytes() throws Exception {
        for (byte[] original : new byte[][] {
            new byte[0], {0, -1, -128, 65}, "\ufeff报告\r\n".getBytes(StandardCharsets.UTF_8)
        }) {
            byte[] decoded = FileSavePlugin.decodeBytes(Base64.encodeToString(original, Base64.NO_WRAP), original.length);
            ByteArrayOutputStream saved = new ByteArrayOutputStream();
            FileSavePlugin.writeAndClose(saved, decoded);
            assertArrayEquals(original, saved.toByteArray());
        }
        assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.decodeBytes("AB==", 1));
        assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.decodeBytes("AA==", 2));
    }
}
