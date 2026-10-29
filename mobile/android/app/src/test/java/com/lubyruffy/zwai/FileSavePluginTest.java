package com.lubyruffy.zwai;

import static org.junit.Assert.*;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.OutputStream;
import org.junit.Test;

public class FileSavePluginTest {
    @Test
    public void selectedBasenameKeepsUnicodeAndWhitespace() {
        assertEquals(" 报告.txt ", FileSavePlugin.checkedName(" 报告.txt "));
        for (String bad : new String[] {"", ".", "..", "../note", "a/b", "a\\b", "a\0b", "a\nb"}) {
            assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkedName(bad));
        }
    }

    @Test
    public void rawSizeRejectsCoercionFractionsAndOverflow() {
        assertEquals(0, FileSavePlugin.checkedSize(0));
        assertEquals(FileSavePlugin.MAX_BYTES, FileSavePlugin.checkedSize((long) FileSavePlugin.MAX_BYTES));
        for (Object bad : new Object[] {null, "1", true, -1, 1.5, Double.NaN, Double.POSITIVE_INFINITY, FileSavePlugin.MAX_BYTES + 1L}) {
            assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkedSize(bad));
        }
    }

    @Test
    public void onlyCanonicalBase64WithTheDeclaredByteSizeIsAccepted() {
        FileSavePlugin.checkBase64("", 0);
        FileSavePlugin.checkBase64("AA==", 1);
        FileSavePlugin.checkBase64("AAA=", 2);
        FileSavePlugin.checkBase64("////", 3);
        for (String bad : new String[] {"AA", "AA=", "AA===", " A==", "AA\n=", "AB==", "AA__"}) {
            assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkBase64(bad, 1));
        }
        assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkBase64("AAB=", 2));
        assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkBase64("AA==", 0));
        assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkBase64("", FileSavePlugin.MAX_BYTES + 1));
    }

    @Test
    public void mimeParametersAreNotPassedToTheDocumentPicker() {
        assertEquals("text/plain", FileSavePlugin.checkedMime("text/plain; charset=utf-8"));
        assertEquals("application/octet-stream", FileSavePlugin.checkedMime("APPLICATION/OCTET-STREAM"));
        assertThrows(IllegalArgumentException.class, () -> FileSavePlugin.checkedMime("not a MIME"));
    }

    @Test
    public void savingPreservesBinaryAndClosesEvenWhenTheProviderFails() throws Exception {
        byte[] source = {0, -1, 1, -128};
        final boolean[] closed = {false};
        ByteArrayOutputStream target = new ByteArrayOutputStream() {
            @Override public void close() { closed[0] = true; }
        };
        FileSavePlugin.writeAndClose(target, source);
        assertArrayEquals(source, target.toByteArray());
        assertTrue(closed[0]);

        closed[0] = false;
        OutputStream failed = new OutputStream() {
            @Override public void write(int value) throws IOException { throw new IOException("write"); }
            @Override public void close() { closed[0] = true; }
        };
        assertThrows(IOException.class, () -> FileSavePlugin.writeAndClose(failed, source));
        assertTrue(closed[0]);
        OutputStream closeFailed = new ByteArrayOutputStream() {
            @Override public void close() throws IOException { throw new IOException("close"); }
        };
        assertThrows(IOException.class, () -> FileSavePlugin.writeAndClose(closeFailed, source));
    }
}
